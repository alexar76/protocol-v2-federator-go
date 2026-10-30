# Узел для другого оператора / Operator node

Это не Python-хаб и не его форк. Процесс написан по `spec.md`, схемам и тест-векторам. Чужой человек забирает этот каталог (или бинарник), свой ключ и свой хост. Выключить узел может только тот, у кого процесс.

This is not the Python hub and not a fork of it. It was written from the protocol spec, schemas, and test vectors. Another person takes this tree (or the binary), their key, and their host. Only whoever runs the process can stop it.

## Что делает / What it does

- читает `/.well-known` семян и проверяет подпись манифеста ключом из этого документа
- чужой узел остаётся в `observed_hubs` и не попадает в поиск
- в `peers` и в поиск попадают только origin из `--admit`
- входящий `POST /ai-market/v2/federation/announce` не делает отправителя доверенным
- адреса из частной сети, loopback и URL с управляющими символами не запрашиваются
- редиректы не следуют сами

Платежи, invoke и эскроу этот процесс не делает. `channel/open` в спецификации переводит токены в эскроу — здесь его нет нарочно, и бинарник сам не вызывает его на modelmarket.dev.

## Запуск / Run

```bash
go build -o federator ./cmd/federator
./federator keygen --out operator.key
# печатает публичный ключ base64; файл 0600, в git не входит

./federator serve \
  --key operator.key \
  --hub-url https://ИМЯ-ОПЕРАТОРА.example \
  --listen 127.0.0.1:8090 \
  --seed https://modelmarket.dev/.well-known/ai-market.json \
  --admit https://modelmarket.dev \
  --name "Имя оператора"
```

`--hub-url` — публичный origin оператора. `--admit` повторяется для каждого origin, которому он доверяет каталог. `--open-admission` по умолчанию выключен.

Исходящий анонс **не** уходит при старте:

```bash
./federator announce \
  --key operator.key \
  --hub-url https://ИМЯ-ОПЕРАТОРА.example \
  --to https://peer.example/ai-market/v2/federation/announce
```

Файл `operator.key` — собственность оператора. Следующему оператору отдают ключ и команду запуска. Исходники Python-хаба не нужны.
