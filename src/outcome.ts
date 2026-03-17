import type { SessionStatus } from "./types.ts";

export interface SessionOutcomeFields {
  outcomeHeadline: string;
  outcomeChanged: string;
  outcomeVerified: string;
  outcomeUncertain: string;
  outcomeNext: string;
}

interface ParsedOutcomeSections {
  changed?: string;
  next?: string;
  uncertain?: string;
  verified?: string;
}

export interface SessionOutcomeSource {
  error: string;
  resultText: string;
  status: SessionStatus;
}

export function deriveSessionOutcome(source: SessionOutcomeSource): SessionOutcomeFields {
  const sections = parseOutcomeSections(source.resultText);
  const firstLine = firstMeaningfulLine(source.resultText);

  if (source.status === "error") {
    const uncertainty = source.error.trim() || "Session failed without an explicit error.";
    return {
      outcomeHeadline: `Failed: ${uncertainty}`.slice(0, 280),
      outcomeChanged: sections.changed ?? "",
      outcomeVerified: sections.verified ?? "",
      outcomeUncertain: sections.uncertain ?? uncertainty,
      outcomeNext: sections.next ?? "Inspect the error and rerun or intervene manually.",
    };
  }

  if (source.status === "complete") {
    const changed = sections.changed ?? firstLine ?? "";
    return {
      outcomeHeadline: changed || "Completed without agent output.",
      outcomeChanged: changed,
      outcomeVerified: sections.verified ?? "",
      outcomeUncertain: sections.uncertain ?? "",
      outcomeNext: sections.next ?? "",
    };
  }

  return {
    outcomeHeadline: firstLine ?? `Session is ${source.status}.`,
    outcomeChanged: sections.changed ?? "",
    outcomeVerified: sections.verified ?? "",
    outcomeUncertain: sections.uncertain ?? "",
    outcomeNext: sections.next ?? "",
  };
}

function parseOutcomeSections(text: string): ParsedOutcomeSections {
  const sections: ParsedOutcomeSections = {};
  let current: keyof ParsedOutcomeSections | null = null;
  const buffers: Partial<Record<keyof ParsedOutcomeSections, string[]>> = {};

  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    const heading = parseSectionHeading(line);
    if (heading) {
      current = heading.key;
      buffers[current] = [heading.value];
      continue;
    }

    if (!current) {
      continue;
    }

    if (!buffers[current]) {
      buffers[current] = [];
    }
    buffers[current]!.push(line);
  }

  for (const key of Object.keys(buffers) as Array<keyof ParsedOutcomeSections>) {
    const value = normalizeSectionValue(buffers[key]!.join("\n"));
    if (value) {
      sections[key] = value;
    }
  }

  return sections;
}

function parseSectionHeading(
  line: string,
): { key: keyof ParsedOutcomeSections; value: string } | null {
  const match = line.match(/^(Changed|Verified|Uncertain|Next):\s*(.*)$/i);
  if (!match) {
    return null;
  }

  const label = match[1]!.toLowerCase();
  const value = match[2] ?? "";

  switch (label) {
    case "changed":
      return { key: "changed", value };
    case "verified":
      return { key: "verified", value };
    case "uncertain":
      return { key: "uncertain", value };
    case "next":
      return { key: "next", value };
    default:
      return null;
  }
}

function firstMeaningfulLine(text: string): string | undefined {
  for (const line of text.split("\n")) {
    const normalized = normalizeSectionValue(line);
    if (normalized) {
      return normalized;
    }
  }
  return undefined;
}

function normalizeSectionValue(raw: string | undefined): string | undefined {
  if (!raw) {
    return undefined;
  }

  const normalized = raw
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .join(" ");

  return normalized || undefined;
}
