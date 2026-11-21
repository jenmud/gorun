// Screenshots the History pages of gorun -serve, for record.sh.
//   node ciweb.mjs <base url> <out dir>
import { chromium } from "playwright-core";
const [base, out] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const p = await (await b.newContext({ viewport: { width: 1100, height: 680 }, deviceScaleFactor: 2, timezoneId: "UTC" })).newPage();
await p.goto(base + "/history"); await p.waitForTimeout(1500);
await p.screenshot({ path: `${out}/history.png` });
await p.click("a[href^='/history/']"); await p.waitForTimeout(1500);
await p.screenshot({ path: `${out}/run-page.png` });
await p.locator("summary:has-text('Who and where')").first().click(); await p.waitForTimeout(800);
await p.screenshot({ path: `${out}/run-origin.png` });
await b.close();
