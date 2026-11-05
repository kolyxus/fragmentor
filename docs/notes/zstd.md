# klauspost/compress/zstd

Изучено 2026-11-03: исходники `github.com/klauspost/compress v1.20.3` (`zstd/decoder.go`, `decoder_options.go`,
`framedec.go`, `README.md`). Используется в `bot/internal/demo/decompress.go` для потоковой распаковки `.dem.zst`.

## Как распаковываем

```go
dec, err := zstd.NewReader(r,
	zstd.WithDecoderConcurrency(1),          // синхронно, в той же горутине, без фоновых
	zstd.WithDecoderLowmem(true),
	zstd.WithDecoderMaxWindow(32<<20),       // окно и память декодера; у FACEIT окно 2 МиБ
	zstd.WithDecoderMaxMemory(uint64(limit)), // кадр, который объявляет больше, отвергается сразу
	zstd.WithDecodeBuffersBelow(0),          // см. подводные камни
)
defer dec.Close() // обязательно: отпускает буферы (и горутины при concurrency > 1)
// dec — io.Reader с распакованной демкой; поверх него — свой счётчик байтов с ErrTooLarge
```

- Поток распаковывается по мере чтения, на диск ничего не пишется. Распаковка демки FACEIT
  (233 МиБ) отдельно от разбора занимает 0,35 с, Go-процесс — 10 МиБ.
- По умолчанию декодер параллельный: до 4 блоков вперёд в фоновых горутинах. Нам это не нужно —
  разбор демки в 10 раз медленнее распаковки.

## Ошибки

- `zstd.ErrMagicMismatch` — вход не zstd (у нас `demo.ErrNotZstd`).
- `zstd.ErrWindowSizeExceeded` — заголовок кадра просит окно больше `WithDecoderMaxWindow`.
- `zstd.ErrDecoderSizeExceeded` — кадр объявляет размер больше `WithDecoderMaxMemory` (у нас `demo.ErrTooLarge`).
- Обрезанный поток — `io.ErrUnexpectedEOF`. Его передаём парсеру как есть, и demoinfocs считает демку обрезанной.

## Подводные камни

- `WithDecoderMaxMemory` для потока ограничивает только окно и объявленный размер кадра, а не сумму
  распакованного. Сумму ограничивает наш счётчик в `Read`.
- Входы с методами `Bytes()` и `Len()` (`*bytes.Buffer`) меньше `WithDecodeBuffersBelow` (по умолчанию 128 КиБ)
  декодер распаковывает целиком за раз, в обход потокового чтения. 128 КиБ zstd могут развернуться в гигабайты,
  поэтому ставим 0 — так это отключено.
- Заголовок кадра: magic `28 B5 2F FD`, байт флагов, байт окна `(e << 3) | m` — окно `2^(10+e)` плюс
  `m/8` от него. Тест `TestDecompressWindowTooLarge` собирает такой кадр руками.
- У демок FACEIT в заголовке есть размер распакованного (Frame Content Size): 243 939 176 и 355 423 009 байт,
  окно 2 МиБ, один кадр.

## Ссылки

- https://github.com/klauspost/compress/tree/master/zstd — README, раздел «Decompressor»
- https://pkg.go.dev/github.com/klauspost/compress/zstd
- https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md — формат кадра
