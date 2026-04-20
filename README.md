# Go Lavilas Alpha

`Go Lavilas Alpha` - это отдельный NV-продукт для go-реализации `lavilas`, с отдельным alpha-каналом и своим packaging-контуром. На alpha-ветке внешняя команда и артефакты называются `lvls`, чтобы не конфликтовать с основным `lavilas`.

Текущее состояние:
- package id в `nv`: `lvls`;
- publish-контур целится в `nv`, не в `npm`;
- alpha foundation собирает `linux/amd64`, `linux/arm64` и `windows/amd64`;
- release metadata складывается в `dist/SHA256SUMS` и `dist/release-metadata.json`;
- цель ветки - постепенно довести `Go Lavilas` до функционального паритета с основным `lavilas`.

## Каналы

- `alpha` - текущий go-канал
- `beta` - будет добавлен после стабилизации базового UX
- `latest` - только после достижения достаточного паритета

## Локальный запуск

```bash
go run ./cmd/lvls --version
go run ./cmd/lvls help
go run ./cmd/lvls resume
```

## Сборка alpha-артефактов

```bash
LAVILAS_VERSION=0.1.0-alpha.local \
./scripts/build-release.sh
```

После сборки в `dist/` будут:
- `lvls-linux-amd64.tar.gz`
- `lvls-linux-arm64.tar.gz`
- `lvls-windows-amd64.exe`
- `SHA256SUMS`
- `release-metadata.json`

## Подготовка publish-manifest

```bash
LAVILAS_VERSION=0.1.0-alpha.local \
./scripts/prepare-nv-manifest.sh
```

Скрипт генерирует `dist/nv.package.publish.json` с актуальной версией релиза и абсолютными путями до артефактов/README.

## Публикация в NV

```bash
nv publish --manifest ./dist/nv.package.publish.json --tag alpha
```

## GitHub Actions

Workflow `.github/workflows/nv-go-alpha.yml`:
- вычисляет alpha-версию автоматически или принимает manual override;
- собирает branded alpha artifacts для `lvls`;
- кладёт внутрь portable-артефактов бинарь `lvls`;
- прикладывает checksums и release metadata в общий `dist/` artifact;
- публикует в NV только через подготовленный publish-manifest.
