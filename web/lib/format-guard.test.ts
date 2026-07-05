import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

// Production UI must format money/dates through web/lib/format.ts so currency
// symbols, separators and precision stay consistent. Chart tooltips
// (components/charts) are non-monetary axis labels and stay whitelisted, as
// does lib/format.ts itself (scanned roots below exclude lib/).
const ROOTS = ["app", "components"];
const WHITELIST = [/^components\/charts\//];

function collectTsxFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...collectTsxFiles(full));
    } else if (entry.name.endsWith(".tsx") && !entry.name.endsWith(".test.tsx")) {
      out.push(full);
    }
  }
  return out;
}

describe("formatting goes through lib/format", () => {
  it("has no hand-rolled zh-CN toLocaleString in production tsx", () => {
    const offenders: string[] = [];
    for (const root of ROOTS) {
      for (const file of collectTsxFiles(join(process.cwd(), root))) {
        const rel = relative(process.cwd(), file);
        if (WHITELIST.some((re) => re.test(rel))) continue;
        const source = readFileSync(file, "utf8");
        if (source.includes('.toLocaleString("zh-CN"')) {
          offenders.push(rel);
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});
