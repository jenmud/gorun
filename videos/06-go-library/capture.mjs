// Drives the Service Deck example (examples/servicedeck) with Playwright and screenshots a run, for record.sh.
//   node capture.mjs <base url> <out dir>
import { chromium } from "playwright-core";
const [base, out] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await b.newContext({ viewport: { width: 1100, height: 760 }, deviceScaleFactor: 2 });
const p = await ctx.newPage();
await p.goto(base);
await p.waitForTimeout(1000);
await p.click("button:has-text('Deploy API 2.4')");
await p.waitForTimeout(2300);
await p.screenshot({ path: `${out}/deck-running.png` });
await p.waitForTimeout(9000);
await p.screenshot({ path: `${out}/deck-done.png` });
await b.close();
