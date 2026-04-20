# Go Lavilas Alpha

Это независимый orphan-поток для go-переписывания `lavilas` под слабое железо и быстрый install через `nv`.

Текущее состояние:
- publish-контур целится в `nv`, не в `npm`;
- основной канал сейчас `alpha`;
- цель ветки — постепенно довести `Go Lavilas` до функционального паритета с основным `lavilas`.

## Каналы

- `alpha` — текущий go-канал
- `beta` — будет добавлен после стабилизации базового UX
- `latest` — только после достижения достаточного паритета

## Локальный запуск

```bash
go run ./cmd/lavilas --version
go run ./cmd/lavilas help
go run ./cmd/lavilas resume
```

## Сборка артефактов

```bash
./scripts/build-release.sh
```

## Публикация в NV

```bash
nv publish --manifest ./nv.package.json --tag alpha
```
