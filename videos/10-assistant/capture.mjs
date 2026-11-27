// Drives gorun's web UI with Playwright and screenshots the assistant, for record.sh.
//   node capture.mjs <base url> <out dir>
// The replies are the built-in model's own; each question waits until it has answered.
import { writeFileSync } from "node:fs";
import { chromium } from "playwright-core";
const [base, out] = process.argv.slice(2);
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await b.newContext({ viewport: { width: 1100, height: 720 }, deviceScaleFactor: 2, timezoneId: "UTC" });
const p = await ctx.newPage();
p.setDefaultTimeout(300000);
const shot = async (name) => p.screenshot({ path: `${out}/${name}.png` });
const go = async (path, wait = 1200) => { await p.goto(base + path); await p.waitForTimeout(wait); };
const replies = {};
// signIn adds the first user, which turns the login on with this browser signed in, or signs in
// as them when they are there already: running tasks needs a signed-in user.
async function signIn(page) {
  const form = { name: "dev", password: "demo-password" };
  await page.request.post(base + "/users/add", { form });
  await page.request.post(base + "/login", { form: { ...form, next: "/" } });
}
await signIn(p);
const top = async () => { await p.evaluate(() => scrollTo(0, 0)); await p.waitForTimeout(300); };

async function run(servers, pick) {
  await go("/run");
  for (const s of servers) await p.check(`input[name=server][value=${s}]`);
  await p.check(pick);
  await p.click("button:has-text('Run selected')");
  await p.waitForTimeout(9000);
}

// save presses the reply's save button, when it offers one, and screenshots what it said
async function save(name) {
  const add = p.locator("[data-add]");
  if (await add.count()) { await add.last().click(); await p.waitForTimeout(2000); }
  await p.locator("#chat-log > div").last().scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 160));
  await shot(name);
}

// ask sends a question (a suggestion button or typed text) and waits for the reply
async function ask(name, text, suggestion = true) {
  await p.click("#chat-clear");
  if (suggestion) await p.click(`[data-suggest]:has-text(${JSON.stringify(text)})`);
  else { await p.fill("#chat-input", text); await p.press("#chat-input", "Enter"); }
  await p.waitForTimeout(600);
  await shot(`${name}-thinking`);
  await p.waitForFunction(() => document.getElementById("chat-stop").hidden);
  await p.waitForTimeout(400);
  const last = p.locator("#chat-log > div").last();
  replies[name] = await last.innerText();
  await last.scrollIntoViewIfNeeded();
  await p.evaluate(() => scrollBy(0, 120));
  await shot(name);
}

await run(["web-1", "web-2", "web-3", "web-4", "db-1"], "input[name=task][value=disk]");
await run(["web-1", "web-2", "web-3", "web-4"], "input[name=bundle][value=webapp]");
await go("/"); await p.locator("a:has-text('Ask the assistant')").first().scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 200)); await shot("live-done");

await go("/history", 1500); await shot("history");
await p.click("a[href^='/history/']"); await p.waitForTimeout(1500); await shot("run-page");
await p.click("a:has-text('Ask the assistant')"); await p.waitForTimeout(1200); await shot("assistant-run");
await ask("diagnose", "Why did this run fail, and how do I fix it?");

await go("/assistant"); await shot("assistant");
await ask("summary", "Summarise the recent runs");
await ask("servers", "Which tasks run on which servers?");
await top(); await shot("assistant-stats");
await ask("write", "Write a task that installs redis and keeps it running", false);
await ask("complex", "Set up postgres on db-1 with a backup of the app database to /srv/backups, and run the backup every night at 1am", false);
await save("complex-saved");
await go("/tasks"); await shot("tasks-saved");
await go("/assistant");
await ask("sched", "Move nightly-disk to 4:30am, Sydney time", false);
await save("sched-saved");
await go("/schedules"); await shot("schedules");

await go("/config");
await p.click("summary:has-text('Generate tasks')");
await p.fill("textarea[name=request]", "back up /var/www to /srv/backups every night, keeping 7 days");
await p.locator("textarea[name=request]").scrollIntoViewIfNeeded(); await shot("generate-form");
await Promise.all([p.waitForNavigation(), p.click("button:has-text('Generate')")]);
await p.waitForTimeout(1200); await shot("generate-done");

// the Models panel: what is downloaded, and a Hugging Face search (nothing is downloaded)
await go("/assistant");
await p.evaluate(() => { document.getElementById("assistant-models").open = true; });
await p.locator("#assistant-models").scrollIntoViewIfNeeded(); await p.evaluate(() => scrollBy(0, 120)); await shot("models-panel");
await p.fill("[data-hub-search] input[name=q]", "qwen3 4b instruct");
await p.click("[data-hub-search] button");
await p.waitForSelector("#hub-results [data-hub-repo]", { timeout: 30000 }); await p.waitForTimeout(500);
await p.locator("#hub-results").scrollIntoViewIfNeeded(); await shot("models-search");
await p.click("#hub-results [data-hub-repo] >> nth=0");
await p.waitForSelector("#hub-results [data-hub-download], #hub-results [data-hub-back]", { timeout: 30000 }); await p.waitForTimeout(500);
await p.locator("#hub-results").scrollIntoViewIfNeeded(); await shot("models-files");

// training data: capture on, one answer rated, then the page that lists what was kept
await go("/assistant/captures"); await p.click("button:has-text('Turn on')"); await p.waitForTimeout(800);
await go("/assistant");
await ask("training-ask", "Write a task that installs htop", false);
await p.locator("#chat-log > div").last().locator("button:has-text('👍')").click(); await p.waitForTimeout(500);
await shot("training-chat");
await go("/assistant/captures", 1000); await p.locator("main li details").first().evaluate(d => d.open = true); await shot("training-page");

writeFileSync(`${out}/replies.json`, JSON.stringify(replies, null, 2));
await b.close();
