# discuss-watcher

Polls any Fandom-compatible `/wikia.php` endpoint and forwards new threads and replies to Discord webhooks.

Based on [RcGcDb](https://gitlab.com/chicken-riders/RcGcDb).

## Build

Requires Go 1.27+.

```
go build -trimpath -ldflags="-s -w" .
```

## Run

```
discuss-watcher -wiki https://tds.wiki -webhook https://discord.com/api/webhooks/ID/TOKEN
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-wiki` | required | Wiki base URL |
| `-webhook` | required | Discord webhook URL, repeat or comma-separate |
| `-interval` | `30s` | Time between polls |
| `-limit` | `20` | Posts per poll (1 to 100) |
| `-state` | `discuss-watcher.json` | Last seen post id |
| `-types` | all | `forum`, `wall`, `comments` |
| `-compact` | `false` | Compact messages instead of embeds |
| `-hide-ips` | `false` | Hide anonymous IPs |
| `-dry-run` | `false` | Print payloads instead of sending |
| `-once` | `false` | Poll once and exit |
| `-timeout` | `15s` | HTTP timeout |
| `-v` | `false` | Debug logging |

## License

[GPLv3](LICENSE)
