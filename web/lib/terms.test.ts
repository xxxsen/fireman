// @vitest-environment node

import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { HELP_TOPICS, type HelpTopicKey } from "./terms";

const WEB_ROOT = process.cwd();

function sourceFiles(directory: string): string[] {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(absolute);
    if (!entry.name.endsWith(".tsx") || entry.name.endsWith(".test.tsx")) return [];
    return [absolute];
  });
}

const interfaceSources = sourceFiles(path.join(WEB_ROOT, "app"))
  .concat(sourceFiles(path.join(WEB_ROOT, "components")))
  .map((file) => ({ file, source: fs.readFileSync(file, "utf8") }));

describe("help topic registry", () => {
  it("gives every topic a user-facing label and summary", () => {
    for (const [key, topic] of Object.entries(HELP_TOPICS)) {
      expect(topic.label.trim(), `${key} label`).not.toBe("");
      expect(topic.summary.trim(), `${key} summary`).not.toBe("");
    }
  });

  it("gives every calculated topic its calculation, inputs and interpretation", () => {
    for (const [key, topic] of Object.entries(HELP_TOPICS)) {
      if (topic.calculation == null) continue;
      expect(topic.calculation.trim(), `${key} calculation`).not.toBe("");
      expect(topic.inputs?.trim(), `${key} inputs`).toBeTruthy();
      expect(topic.interpretation?.trim(), `${key} interpretation`).toBeTruthy();
    }
  });

  it("does not map one Chinese label to conflicting definitions", () => {
    const seen = new Map<string, { key: string; summary: string }>();
    for (const [key, topic] of Object.entries(HELP_TOPICS)) {
      const previous = seen.get(topic.label);
      if (previous) {
        expect(
          topic.summary,
          `label ${topic.label} is shared by ${previous.key} and ${key}`,
        ).toBe(previous.summary);
      } else {
        seen.set(topic.label, { key, summary: topic.summary });
      }
    }
  });

  it("mounts every registered topic in a non-test interface", () => {
    const unused = (Object.keys(HELP_TOPICS) as HelpTopicKey[]).filter(
      (key) => !interfaceSources.some(({ source }) =>
        source.includes(`termKey="${key}"`) ||
        source.includes(`termKey={'${key}'}`) ||
        source.includes(`termKey={\"${key}\"}`) ||
        source.includes(`'${key}'`) ||
        source.includes(`\"${key}\"`),
      ),
    );
    expect(unused, "orphan help topics need a mounted help entry or deletion").toEqual([]);
  });

  it.each([
    ["components/plans/settings/AnalysisContent.tsx", ["monte_carlo", "wilson_interval", "p_quantiles", "p95_drawdown"]],
    ["components/plans/improvement/ImprovementPage.tsx", ["wilson_lower_bound", "common_random_numbers", "paired_path_changes", "search_boundary"]],
    ["components/plans/frontier/FrontierPage.tsx", ["wilson_lower_bound", "evaluation_paths", "discrete_search_step", "search_domain"]],
    ["app/research/collections/[id]/runs/[runId]/page.tsx", ["metric_cagr", "metric_var_loss", "metric_cvar_loss"]],
    ["components/plans/settings/ParametersContent.tsx", ["inflation_phi", "random_inflation_ar1", "random_seed"]],
    ["components/assumptions/ProfileEditor.tsx", ["correlation_rho", "psd_repair", "return_prior"]],
  ])("keeps high-risk help mounted in %s", (relativeFile, requiredKeys) => {
    const source = fs.readFileSync(path.join(WEB_ROOT, relativeFile), "utf8");
    for (const key of requiredKeys) expect(source, `${relativeFile}: ${key}`).toContain(key);
  });
});
