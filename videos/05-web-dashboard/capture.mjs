// Drives gorun's web UI with Playwright and screenshots it, for record.sh.
//   node capture.mjs <base url> <out dir>
import { chromium } from "playwright-core";
const [base, out] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await b.newContext({ viewport: { width: 1100, height: 680 }, deviceScaleFactor: 2, timezoneId: "UTC" });
const p = await ctx.newPage();
const shot = async (name) => p.screenshot({ path: `${out}/${name}.png` });
const go = async (path, wait = 1200) => { await p.goto(base + path); await p.waitForTimeout(wait); };

await go("/"); await shot("live-empty");
await go("/run");
for (const s of ["web-1", "web-2", "web-3", "web-4"]) await p.check(`input[name=server][value=${s}]`);
await p.check("input[name=bundle][value=webapp]");
await shot("run-picker");
await p.click("button:has-text('Run selected')");
await p.waitForTimeout(3000);
const click = async (sel) => { await p.locator(sel).first().click().catch(() => {}); await p.waitForTimeout(500); };
const top = async () => { await p.evaluate(() => scrollTo(0, 0)); };
await click("details.group\\/run > summary"); await shot("live-run");
await click("details.group\\/srv:has-text('web-1') > summary"); await shot("live-srv");
await p.waitForTimeout(16000);
await top(); await shot("live-done");
await click("details.group\\/srv:has-text('web-3') > summary"); await p.locator("details.group\\/srv:has-text('web-3')").scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, -140)); await shot("live-fail");
await go("/history", 1500); await shot("history");
await p.click("a[href^='/history/']"); await p.waitForTimeout(1500); await shot("run-page");
await p.getByText("exit 7").first().scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 220)); await p.waitForTimeout(400); await shot("run-fail");
await go("/tasks"); await shot("tasks"); await click("summary:has-text('smoke')"); await shot("tasks-open");
await go("/bundles"); await shot("bundles");
await go("/schedules"); await shot("schedules");
await go("/config"); await shot("config");
await b.close();
