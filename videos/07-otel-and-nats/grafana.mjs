// Screenshots Grafana's Explore for traces, metrics and logs, for record.sh.
//   node grafana.mjs <grafana url> <out dir>
import { chromium } from "playwright-core";
const [base, out] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await b.newContext({ viewport: { width: 1280, height: 1500 }, deviceScaleFactor: 2, timezoneId: "UTC" });
const p = await ctx.newPage();
const explore = (ds, type, query, extra = {}) => `${base}/explore?schemaVersion=1&orgId=1&panes=` + encodeURIComponent(JSON.stringify({
  a: { datasource: ds, queries: [{ refId: "A", datasource: { type, uid: ds }, ...extra, ...(type === "tempo" ? { queryType: "traceql", query } : { expr: query }) }], range: { from: "now-30m", to: "now" } } }));
// the result panel only: from the anchor text down, without Grafana's menus and query editor
const shot = async (name, anchor, h = 1000, nth = 0, left = null) => {
  const loc = p.getByText(anchor, { exact: true });
  const box = await (nth < 0 ? loc.last() : loc.nth(nth)).boundingBox();
  const y = Math.max(0, box.y - 14), x0 = left ?? Math.max(0, box.x - 16);
  await p.screenshot({ path: `${out}/${name}.png`, clip: { x: x0, y, width: 1280 - x0 - 8, height: Math.min(1500 - y, h) } });
};

// traces: the runs, then one run's waterfall
await p.goto(explore("tempo", "tempo", '{ resource.service.name = "gorun" && name = "gorun.run" }'));
await p.waitForTimeout(5000); await shot("traces-search", "Table - Traces", 330);
const id = await p.evaluate(async (base) => {
  const r = await fetch(`${base}/api/datasources/proxy/uid/tempo/api/search?q=${encodeURIComponent('{ name = "gorun.run" }')}&limit=5`);
  return (await r.json()).traces[0].traceID;
}, base);
await p.goto(explore("tempo", "tempo", id));
await p.waitForTimeout(5000); await shot("trace", "Trace", 760);
// open the run span's attributes
await p.locator("text=gorun.run").nth(1).click().catch(() => {});
await p.waitForTimeout(800);
// fold every group but "Other", where gorun's own attributes are
await p.evaluate(() => document.querySelectorAll('[data-testid^="attribute-category-"]').forEach((c) => {
  const b = c.querySelector("button");
  if (c.getAttribute("data-testid") !== "attribute-category-other" && b?.getAttribute("aria-expanded") === "true") b.click();
}));
await p.waitForTimeout(1200); await shot("trace-attrs", "Service & Operation", 800);

// metrics
await p.goto(explore("prometheus", "prometheus", "sum by (server_name, task_status) (increase(gorun_task_executions_total[30m]))"));
await p.waitForTimeout(4000); await shot("metrics-count", "Graph", 460, -1);
await p.goto(explore("prometheus", "prometheus", "histogram_quantile(0.95, sum by (le, task_name) (rate(gorun_task_duration_seconds_bucket[5m])))"));
await p.waitForTimeout(4000); await shot("metrics-duration", "Graph", 460, -1);

// logs: gorun's own log records, sent over OTLP
await p.goto(explore("loki", "loki", '{service_name="gorun"} | detected_level = "error"'));
await p.waitForTimeout(5000); await shot("logs-errors", "Logs volume", 760, 0, 490);
await b.close();
