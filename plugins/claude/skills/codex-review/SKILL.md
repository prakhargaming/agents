---
name: codex-review
description: Get an adversarial, second-opinion code review of the current branch from the OpenAI `codex` CLI, then present its findings to the user for triage. Use this whenever the user types /codex-review, asks for "a codex review", "have codex look at this", "second opinion from codex", "adversarial review", or wants another model to check a branch for logic bugs before a PR. It feeds codex the ticket and the planning decisions so the review judges the change against what was intended.
---

# Codex review

Codex reviews the branch. You collect context, run codex, and present its findings. The user decides which findings to fix. Do not fix anything in this skill.

## 1. Pick the diff

- Use the base the user names, if any.
- Otherwise use the PR base: `gh pr view --json baseRefName -q .baseRefName`. Stacked branches often target another branch, not `main`, so this matters.
- If there is no PR, use `main`.
- If the work is uncommitted, tell the user. Codex reviews `merge-base...HEAD`, so it does not see uncommitted changes. Ask whether to commit first or to proceed without them.

## 2. Write the context file

Codex has none of this conversation. The context is what makes the review adversarial against the intent, not only against the code. Write a file in the scratchpad (for example `codex-review-context.md`) with:

- **Ticket.** Find the ticket id in the branch name, the commit messages, or the conversation (for example `AIG-2144`). Fetch it with the Linear MCP (`get_issue`) and include the title, description, and acceptance criteria. Include the ids of linked parent or sibling tickets only when they define scope.
- **Planning decisions.** Gather decisions from this conversation, from an approved plan file (`~/.claude/plans/`), and from relevant memory files. Write each decision as a short fact, with the reason when you know it. Include rejected alternatives that codex might otherwise propose, so codex does not re-argue them.
- **Known limits.** Write down what is out of scope on purpose and what follow-up tickets will cover.

Keep it factual and short. If you cannot find a ticket or plan, say so in the file, and tell the user that the review has less context.

## 3. Run codex

```bash
~/.claude/skills/codex-review/scripts/run_codex_review.sh <repo-root> <base-ref> <context-file> <scratchpad>/codex-review.md
```

The script runs `codex exec` in a read-only sandbox. It passes `references/codex-instructions.md` as codex developer instructions (its system prompt). Those instructions tell codex to:

- review for logical correctness,
- not run tests, builds, or linters, because CI runs them,
- list findings from least to most important,
- use the ticket and planning context as the intended behavior.

A review can take more than ten minutes. Run the script with `run_in_background` and wait for the notification. Do not poll it. Tell the user the review is running. If it fails, read `<out-file>.log` and report the error. Do not retry with weaker sandbox flags.

## 4. Present the findings

Present every finding. Do not drop findings, and do not merge them. Keep codex's order (least to most important), so the most important finding is last and nearest the user's reply. State the order once at the top.

For each finding, show:

- the number, the severity, and `path:line`,
- what is wrong and the scenario that breaks, in plain words,
- codex's proposed fix in one line,
- **Claude's take:** one line from you. Open the cited code and check the claim. Say whether it holds, is wrong, or is already handled, with a short reason. A false finding costs the user time, so this check matters. Keep your take separate from codex's claim.

Write for a human who wants to decide quickly. Use short sentences and the active voice, and cut jargon. This follows the `simplified-technical-english` rules and the spirit of `/bro`: restate codex in the way one engineer talks to another. Do not paste codex's raw text unless the user asks. The raw output stays at `<scratchpad>/codex-review.md`. Give the user that full path.

End with a one-line count, for example "6 findings: 1 major, 3 minor, 2 nits. I think 4 are real." Then ask which findings to address. Wait for the user before you change code.
