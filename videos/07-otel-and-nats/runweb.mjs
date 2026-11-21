// Starts runs from the web UI's Run page, for record.sh.
//   node runweb.mjs <base url> <number of runs>
import { chromium } from "playwright-core";
const [base, n] = [process.argv[2], Number(process.argv[3] || 1)];
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const p = await (await b.newContext({ viewport: { width: 1100, height: 680 } })).newPage();
for (let i = 0; i < n; i++) {
  await p.goto(base + "/run"); await p.waitForTimeout(800);
  for (const s of ["web-1", "web-2", "web-3", "web-4"]) await p.check(`input[name=server][value=${s}]`);
  await p.check("input[name=bundle][value=webapp]");
  await p.click("button:has-text('Run selected')");
  await p.waitForTimeout(24000);
}
await b.close();
