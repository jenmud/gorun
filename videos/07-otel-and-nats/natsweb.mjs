// Screenshots gorun's web UI header (how it reaches NATS) and the Live page, for record.sh.
//   node natsweb.mjs <base url> <out dir> <name>
import { chromium } from "playwright-core";
const [base, out, name] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const p = await (await b.newContext({ viewport: { width: 1100, height: 680 }, deviceScaleFactor: 2, timezoneId: "UTC" })).newPage();
await p.goto(base + "/"); await p.waitForTimeout(1500);
await p.screenshot({ path: `${out}/${name}-header.png`, clip: { x: 0, y: 0, width: 1100, height: 90 } });
if (name === "embedded") {
  await p.waitForTimeout(500);
  await p.screenshot({ path: `${out}/${name}-live.png` });
}
await b.close();
