VERDICT: PASS

- [nit] cmd/web/ui/config.templ:74 and cmd/web/ui/view.go:441: text-only change (`gorun -web` to `gorun -serve`). No class or layout change, so phone width and dark mode are unaffected.
- [nit] cmd/web/ui/config_templ.go: contains the `-serve` text; the rest is v0.3.1070 churn (attribute values now go through `templ.ResolveAttributeValue`, which escapes, so dropping `templ.EscapeString` is not a regression).
- [nit] cmd/websrv/startup.go: plain stdout with aligned labels. No TUI view changed.
