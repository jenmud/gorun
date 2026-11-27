// Drives gorun's web UI with Playwright and screenshots it, for record.sh.
//   node capture.mjs <base url> <out dir> <record dir>
// gorun -serve starts with no Gorunfile, so it keeps them in its database: the capture creates
// "base" and "web" there and imports record/base.toml and record/Gorunfile into them.
import { chromium } from "playwright-core";
const [base, out, rec] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await b.newContext({ viewport: { width: 1100, height: 680 }, deviceScaleFactor: 2, timezoneId: "UTC" });
const p = await ctx.newPage();
p.on("dialog", (d) => d.accept()); // Import and Delete ask first
const shot = async (name) => p.screenshot({ path: `${out}/${name}.png` });
const go = async (path, wait = 1200) => { await p.goto(base + path); await p.waitForTimeout(wait); };
const click = async (sel) => { await p.locator(sel).first().click().catch(() => {}); await p.waitForTimeout(500); };
const top = async () => { await p.evaluate(() => scrollTo(0, 0)); };

// create opens a new stored Gorunfile and imports file into it
async function create(name, file) {
  await go("/gorunfiles");
  await p.fill("form[action='/gorunfiles/new'] input[name=name]", name);
  await p.click("button:has-text('Create and open')"); await p.waitForURL(/\/config/, { waitUntil: "domcontentloaded" });
  await p.setInputFiles("form[action='/config/import'] input[type=file]", `${rec}/${file}`);
  await p.click("form[action='/config/import'] button:has-text('Import')"); await p.waitForURL(/flash=Imported/, { waitUntil: "domcontentloaded" });
  await p.waitForTimeout(800);
}

await go("/gorunfiles"); await shot("gorunfiles-first");
await create("base", "base.toml");
await create("web", "Gorunfile");
await top(); await shot("config-imported");
await go("/gorunfiles");
await p.click("form[action='/gorunfiles/delete']:has(input[value=default]) button", { timeout: 5000 }).catch(() => {}); await p.waitForTimeout(1000);
await go("/gorunfiles"); await shot("gorunfiles");
await go("/config");
await p.locator("text=Switch or create").first().scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 160)); await p.waitForTimeout(300);
await shot("config-gorunfile");

// two runs that pass first, so Live's stats and Recent runs have something to show
async function run(servers, pick, wait) {
  await go("/run");
  for (const s of servers) await p.check(`input[name=server][value=${s}]`);
  await p.check(pick);
  await p.click("button:has-text('Run selected')");
  await p.waitForTimeout(wait);
}
await run(["db-1", "web-4"], "input[name=task][value=disk]", 4000);
await run(["db-1"], "input[name=task][value=update]", 9000);
await p.click("button:has-text('Clear finished')").catch(() => {}); await p.waitForTimeout(800);
await go("/"); await shot("live-empty");
await go("/run");
for (const s of ["web-1", "web-2", "web-3", "web-4"]) await p.check(`input[name=server][value=${s}]`);
await p.check("input[name=bundle][value=webapp]");
await shot("run-picker");
await p.click("button:has-text('Run selected')");
await p.waitForTimeout(3000);
await click("details.group\\/run > summary"); await shot("live-run");
await click("details.group\\/srv:has-text('web-1') > summary"); await shot("live-srv");
await p.waitForTimeout(16000);
await top(); await shot("live-done");
await click("details.group\\/srv:has-text('web-3') > summary"); await p.locator("details.group\\/srv:has-text('web-3')").scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, -140)); await shot("live-fail");
await go("/history", 1500); await shot("history");
await p.click("a[href^='/history/']"); await p.waitForTimeout(1500); await shot("run-page");
await p.getByText("exit 7").first().scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 220)); await p.waitForTimeout(400); await shot("run-fail");
await go("/tasks"); await shot("tasks"); await click("summary:has-text('nginx')"); await shot("tasks-open");
await go("/bundles"); await shot("bundles");
await go("/schedules"); await shot("schedules");
await go("/assistant"); await shot("assistant");
await b.close();
