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

async function run(servers, pick) {
  await go("/run");
  for (const s of servers) await p.check(`input[name=server][value=${s}]`);
  await p.check(pick);
  await p.click("button:has-text('Run selected')");
  await p.waitForTimeout(9000);
}

// ask sends a question (a suggestion button or typed text) and waits for the reply
async function ask(name, text, suggestion = true) {
  await p.click("#chat-clear");
  if (suggestion) await p.click(`[data-suggest]:has-text(${JSON.stringify(text)})`);
  else { await p.fill("#chat-input", text); await p.press("#chat-input", "Enter"); }
  await p.waitForTimeout(600);
  await shot(`${name}-thinking`);
  await p.waitForFunction(() => !document.getElementById("chat-send").disabled);
  await p.waitForTimeout(400);
  const last = p.locator("#chat-log > div").last();
  replies[name] = await last.innerText();
  await last.scrollIntoViewIfNeeded();
  await p.evaluate(() => scrollBy(0, 120));
  await shot(name);
}

await run(["web-1", "web-2", "web-3", "web-4", "db-1"], "input[name=task][value=disk]");
await run(["web-1", "web-2", "web-3", "web-4"], "input[name=bundle][value=webapp]");
await go("/"); await shot("live-done");

await go("/history", 1500); await shot("history");
await p.click("a[href^='/history/']"); await p.waitForTimeout(1500); await shot("run-page");
await p.click("a:has-text('Ask the assistant')"); await p.waitForTimeout(1200); await shot("assistant-run");
await ask("diagnose", "Why did this run fail, and how do I fix it?");

await go("/assistant"); await shot("assistant");
await ask("summary", "Summarise the recent runs");
await ask("servers", "Which tasks run on which servers?");
await ask("write", "Write a task that installs redis and keeps it running", false);
await p.locator("[data-add]").last().click(); await p.waitForTimeout(1500); await shot("write-added");
await go("/config"); await shot("config-unsaved");

await go("/config");
await p.click("summary:has-text('Generate tasks')");
await p.fill("textarea[name=request]", "back up /var/www to /srv/backups every night, keeping 7 days");
await p.locator("textarea[name=request]").scrollIntoViewIfNeeded(); await shot("generate-form");
await Promise.all([p.waitForNavigation(), p.click("button:has-text('Generate')")]);
await p.waitForTimeout(1200); await shot("generate-done");

writeFileSync(`${out}/replies.json`, JSON.stringify(replies, null, 2));
await b.close();
