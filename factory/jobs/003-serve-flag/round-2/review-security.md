VERDICT: CHANGES

- [must] cmd/websrv/nats.go:30 (with :47): the `off (<reason>)` text still leaks credentials when
  `GORUN_NATS_CLIENT_URL` does not parse. nats.go returns the raw `*url.Error` from `url.Parse`
  (vendor nats.go:2250, via `setupServerPool`), and `internal/nats/client.go:62` wraps it with `%w`.
  That error quotes the URL, and `userinfoRE` (`[^/\s@]*@`) stops at the first `/`. A password or
  token with an unencoded `/` (common in base64 secrets) is printed whole. For
  `nats://u:a/b@host:4222` the reason is `parse "nats://u:a/b@host:4222": invalid port ":a" after
  host`. A `#` is also a problem: url.Error shows `"nats://u:a"`, which has no `@` to anchor the
  regex, and the inner error still holds `:a`. I checked both with the same regex and `net/url`.
  The reason goes to stdout and to the header and Config page, and those pages have no login on
  `:8080`. Fix: in `natsStartState`, if `errors.As(connectErr, new(*url.Error))`, set the reason to
  a fixed string such as `GORUN_NATS_CLIENT_URL is not a valid URL`, without the URL or the inner
  error. Keep `scrubUserinfo` for other errors, and make its regex run greedily to the last `@`
  before whitespace or a quote (`[^\s"]*@`). Add the `u:a/b@` and `u:a#b@` cases to
  `TestNATSOffReasonHidesCredentials`, built from a real `inats.NewClient()` error.
- [nit] cmd/websrv/nats.go:40: the `url.Parse` fallback in `withoutUserinfo` can't be reached,
  because `ConnectedUrl()` is already `url.URL.String()`. That's harmless. If you keep it, return ""
  on a parse error rather than the regex-scrubbed raw text.
- [nit] internal/nats/client.go:61,65,68 (unchanged): `options.Url` with credentials goes to the
  log file, the OTEL metric attribute and the span attribute. The metric and span attributes
  reach any configured OTLP backend. This is out of scope, but a follow-up should reuse
  `withoutUserinfo` there and at internal/natsserver/natsserver.go:56.

Round-1 must, connected URL: fixed. `withoutUserinfo` sets `u.User = nil`, which drops the user,
the password and a bare token. Both the embedded and external branches use it, so stdout,
`natsStatus()`, the header and the Config page all get the clean URL.
`TestStartupMessageHidesNATSCredentials` checks this against a real server with user:pass and with
a token.

Checked and fine: no new shell, SSH, copy or template-to-shell paths, and no new handlers or GET
mutations. In the regenerated `config_templ.go`, `ResolveAttributeValue` still calls
`EscapeString` for strings, so escaping is unchanged. The bind error on stderr has only the
address. The startup message shows file paths and parse errors, not env secrets. go.mod and
vendor/ are unchanged.
