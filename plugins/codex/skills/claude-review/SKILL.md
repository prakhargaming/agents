---
name: claude-review
description: Get an adversarial, second-opinion code review of the current branch from the Anthropic `claude` CLI, then present its findings to the user for triage. Use this whenever the user types /claude-review, asks for "a claude review", "have claude look at this", "second opinion from claude", "adversarial review", or wants another model to check a branch for logic bugs before a PR. It feeds claude the ticket and the planning decisions so the review judges the change against what was intended.
---

# Claude review

Claude reviews the branch. You collect context, run claude, and present its findings. The user decides which findings to fix. Do not fix anything in this skill.

## 1. Pick the diff

- Use the base the user names, if any.
- Otherwise use the PR base: `gh pr view --json baseRefName -q .baseRefName`. Stacked branches often target another branch, not `main`, so this matters.
- If there is no PR, use `main`.
- If the work is uncommitted, tell the user. Claude reviews `merge-base...HEAD`, so it does not see uncommitted changes. Ask whether to commit first or to proceed without them.

## 2. Write the context file

Claude has none of this conversation. The context is what makes the review adversarial against the intent, not only against the code. Write a file in the scratchpad (for example `claude-review-context.md`) with:

- **Ticket.** Find the ticket id in the branch name, the commit messages, or the conversation (for example `AIG-2144`). Fetch it with an available Linear tool and include the title, description, and acceptance criteria. Include the ids of linked parent or sibling tickets only when they define scope.
- **Planning decisions.** Gather decisions from this conversation, from an approved plan file available in this session, and from relevant memory files. Write each decision as a short fact, with the reason when you know it. Include rejected alternatives that claude might otherwise propose, so claude does not re-argue them.
- **Known limits.** Write down what is out of scope on purpose and what follow-up tickets will cover.

Keep it factual and short. If you cannot find a ticket or plan, say so in the file, and tell the user that the review has less context.

## 3. Run claude

```bash
~/.codex/skills/claude-review/scripts/run_claude_review.sh <repo-root> <base-ref> <context-file> <scratchpad>/claude-review.md
```

The script runs `claude --print` with the configured default model. It appends `references/claude-instructions.md` to the system prompt. It supplies the committed diff and limits Claude to `Read`, `Glob`, and `Grep`. It disables session persistence, customizations, and MCP servers. These are tool restrictions, not an OS sandbox. Those instructions tell Claude to:

- review for logical correctness,
- not run tests, builds, or linters, because CI runs them,
- list findings from least to most important,
- use the ticket and planning context as the intended behavior.

A review can take more than ten minutes. Run the script with the available background execution tool. Wait for completion notifications when supported; otherwise use the tool's session wait mechanism. Do not repeatedly inspect the output file. Tell the user the review is running. If it fails, read `<out-file>.log` and report the error. Do not retry with weaker tool or permission restrictions.

## 4. Present the findings

Present every finding. Do not drop findings, and do not merge them. Keep claude's order (least to most important), so the most important finding is last and nearest the user's reply. State the order once at the top.

For each finding, show:

- the number, the severity, and `path:line`,
- what is wrong and the scenario that breaks, in plain words,
- claude's proposed fix in one line,
- **Codex's take:** one line from you. Open the cited code and check the claim. Say whether it holds, is wrong, or is already handled, with a short reason. A false finding costs the user time, so this check matters. Keep your take separate from claude's claim.

Write for a human who wants to decide quickly. Use short sentences and the active voice, and cut jargon. Restate Claude's findings in the way one engineer talks to another. Do not paste claude's raw text unless the user asks. The raw output stays at `<scratchpad>/claude-review.md`. Give the user that full path.

End with a one-line count, for example "6 findings: 1 major, 3 minor, 2 nits. I think 4 are real." Then ask which findings to address. Wait for the user before you change code.
