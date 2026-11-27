// Starts runs from the web UI's Run page, for record.sh.
//   node runweb.mjs <base url> <number of runs>
import { chromium } from "playwright-core";
const [base, n] = [process.argv[2], Number(process.argv[3] || 1)];
const b = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", args: ["--no-sandbox"] });
const p = await (await b.newContext({ viewport: { width: 1100, height: 680 } })).newPage();
// signIn adds the first user, which turns the login on with this browser signed in, or signs in
// as them when they are there already: running tasks needs a signed-in user.
async function signIn(page) {
  const form = { name: "dev", password: "demo-password" };
  await page.request.post(base + "/users/add", { form });
  await page.request.post(base + "/login", { form: { ...form, next: "/" } });
}
await signIn(p);
for (let i = 0; i < n; i++) {
  await p.goto(base + "/run"); await p.waitForTimeout(800);
  for (const s of ["web-1", "web-2", "web-3", "web-4"]) await p.check(`input[name=server][value=${s}]`);
  await p.check("input[name=bundle][value=webapp]");
  await p.click("button:has-text('Run selected')");
  await p.waitForTimeout(24000);
}
await b.close();
