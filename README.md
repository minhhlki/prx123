# Zmap-ProxyScanner

Fast proxy checker for `http`, `socks4`, and `socks5` inputs.

This version uses a direct worker pool, streamed input, buffered output, optional deduplication, and strict response validation against a configurable endpoint.

## Config

```json
{
  "check-site": "https://check-ec8.pages.dev/ping.txt",
  "expected-status": 200,
  "expected-body": "ok-proxy-check",
  "proxy-type": "http",
  "http_threads": 800,
  "queue_size": 16000,
  "deduplicate_input": false,
  "deduplicate_output": true,
  "headers": {
    "user-agent": "Mozilla/5.0 (compatible; ZmapProxyScanner/2.0; +https://check-ec8.pages.dev/ping.txt)",
    "accept": "text/plain,*/*;q=0.8"
  },
  "print_ips": {
    "enabled": false,
    "display-ip-info": false,
    "max-concurrent": 4,
    "timeout": 5
  },
  "report_proxy": {
    "enabled": false,
    "url": "https://shareproxy.pro/proxy.php",
    "max-concurrent": 4,
    "timeout": 15
  },
  "timeout": {
    "http_timeout": 4,
    "socks4_timeout": 5,
    "socks5_timeout": 5
  }
}
```

## Flags

```shell
-p <port>                Default port when an input line does not include one.
-o <output.txt>          Output file.
-cfg <config.json>       Config path.
-in <proxies.txt>        Load proxies from file.
-url <https://...>       Load proxies from URL.
```

If no `-in` or `-url` is provided, the scanner reads proxies from `stdin`.

## Notes

- `expected-body` lets you verify the response body instead of accepting any `200`.
- `deduplicate_input` trades RAM for fewer duplicate checks.
- `deduplicate_output` keeps the result file clean even when input contains duplicates.
- `print_ips.display-ip-info` uses `ip-api.com` and is intentionally disabled by default because it is not part of the hot path.
- `report_proxy.enabled` is disabled by default. Turn it on only if you explicitly want to POST working proxies to an external endpoint.

## Example

```shell
zmap -p 8080 | ./ZmapProxyScanner -p 8080
```
