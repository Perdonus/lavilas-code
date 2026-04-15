use crate::error::TransportError;
use crate::request::Request;
use http::HeaderMap;
use rand::Rng;
use std::future::Future;
use std::time::Duration;
use tokio::time::sleep;

const DEFAULT_RATE_LIMIT_DELAY: Duration = Duration::from_secs(30);

#[derive(Debug, Clone)]
pub struct RetryPolicy {
    pub max_attempts: u64,
    pub base_delay: Duration,
    pub retry_on: RetryOn,
}

#[derive(Debug, Clone)]
pub struct RetryOn {
    pub retry_429: bool,
    pub retry_5xx: bool,
    pub retry_transport: bool,
}

impl RetryOn {
    pub fn should_retry(&self, err: &TransportError, attempt: u64, max_attempts: u64) -> bool {
        if attempt >= max_attempts {
            return false;
        }
        match err {
            TransportError::Http { status, .. } => {
                (self.retry_429 && status.as_u16() == 429)
                    || (self.retry_5xx && status.is_server_error())
            }
            TransportError::Timeout | TransportError::Network(_) => self.retry_transport,
            _ => false,
        }
    }
}

pub fn backoff(base: Duration, attempt: u64) -> Duration {
    if attempt == 0 {
        return base;
    }
    let exp = 2u64.saturating_pow(attempt as u32 - 1);
    let millis = base.as_millis() as u64;
    let raw = millis.saturating_mul(exp);
    let jitter: f64 = rand::rng().random_range(0.9..1.1);
    Duration::from_millis((raw as f64 * jitter) as u64)
}

fn parse_retry_after(headers: Option<&HeaderMap>) -> Option<Duration> {
    let raw = headers
        .and_then(|headers| headers.get(http::header::RETRY_AFTER))
        .and_then(|value| value.to_str().ok())?;
    let seconds = raw.trim().parse::<u64>().ok()?;
    Some(Duration::from_secs(seconds))
}

fn retry_delay_for_error(policy: &RetryPolicy, err: &TransportError, attempt: u64) -> Duration {
    match err {
        TransportError::Http {
            status,
            headers,
            ..
        } if status.as_u16() == 429 => {
            parse_retry_after(headers.as_ref()).unwrap_or(DEFAULT_RATE_LIMIT_DELAY)
        }
        _ => backoff(policy.base_delay, attempt),
    }
}

pub async fn run_with_retry<T, F, Fut>(
    policy: RetryPolicy,
    mut make_req: impl FnMut() -> Request,
    op: F,
) -> Result<T, TransportError>
where
    F: Fn(Request, u64) -> Fut,
    Fut: Future<Output = Result<T, TransportError>>,
{
    for attempt in 0..=policy.max_attempts {
        let req = make_req();
        match op(req, attempt).await {
            Ok(resp) => return Ok(resp),
            Err(err)
                if policy
                    .retry_on
                    .should_retry(&err, attempt, policy.max_attempts) =>
            {
                sleep(retry_delay_for_error(&policy, &err, attempt + 1)).await;
            }
            Err(err) => return Err(err),
        }
    }
    Err(TransportError::RetryLimit)
}

#[cfg(test)]
mod tests {
    use super::*;
    use http::HeaderValue;
    use http::StatusCode;

    #[test]
    fn retry_delay_for_429_prefers_retry_after_header() {
        let mut headers = HeaderMap::new();
        headers.insert(http::header::RETRY_AFTER, HeaderValue::from_static("7"));
        let err = TransportError::Http {
            status: StatusCode::TOO_MANY_REQUESTS,
            url: None,
            headers: Some(headers),
            body: None,
        };
        let policy = RetryPolicy {
            max_attempts: 4,
            base_delay: Duration::from_millis(200),
            retry_on: RetryOn {
                retry_429: true,
                retry_5xx: true,
                retry_transport: true,
            },
        };

        assert_eq!(retry_delay_for_error(&policy, &err, 1), Duration::from_secs(7));
    }

    #[test]
    fn retry_delay_for_429_defaults_to_thirty_seconds_without_header() {
        let err = TransportError::Http {
            status: StatusCode::TOO_MANY_REQUESTS,
            url: None,
            headers: None,
            body: None,
        };
        let policy = RetryPolicy {
            max_attempts: 4,
            base_delay: Duration::from_millis(200),
            retry_on: RetryOn {
                retry_429: true,
                retry_5xx: true,
                retry_transport: true,
            },
        };

        assert_eq!(
            retry_delay_for_error(&policy, &err, 1),
            Duration::from_secs(30)
        );
    }
}
