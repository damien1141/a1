# Output Enforcement

Complete, unabridged output. No placeholders, no truncation, no "rest of code omitted". A partial output is a broken output — the user asked for a full file, deliver a full file; asked for 5 components, deliver 5 components.

## Baseline

Treat every task as production-critical. Do not optimize for brevity — optimize for completeness. If the user asks for a full file, deliver the full file. If the user asks for 5 components, deliver 5 components. No exceptions.

This is the discipline that separates production-ready output from "looks done but isn't." A response with `// ... existing code ...` is not a code drop; it is a description of a code drop. The user still has to write the code. You have not delivered the work; you have delivered an outline.

## Banned output patterns

The following are hard failures. Never produce them.

### In code blocks

```text
// ...                          ← bare ellipsis standing in for code
// rest of code
// rest of code omitted
// implement here
// TODO: implement
// similar to above
// continue pattern
// add more as needed
/* ... */
...                              ← bare ellipsis as code
```

### In prose

```text
"Let me know if you want me to continue"
"I can provide more details if needed"
"for brevity"
"the rest follows the same pattern"
"similarly for the remaining"
"and so on"                      ← when replacing actual content
"I'll leave that as an exercise"
"trust me, it works"
```

### Structural shortcuts

- Outputting a skeleton when the request was for a full implementation
- Showing the first and last section while skipping the middle
- Replacing repeated logic with one example and a description
- Describing what code should do instead of writing it
- Showing the function signature with `// body` instead of the body

## Execution process

### 1. Scope

Read the full request. Count how many distinct deliverables are expected (files, functions, sections, answers). Lock that number.

```text
Request: "Generate the User service — model, repository, service, controller, tests."
Deliverables: 5 (model, repository, service, controller, tests)
Lock: 5
```

### 2. Build

Generate every deliverable completely. No partial drafts, no "you can extend this later."

```text
- model.ts         ← complete, every field, every type
- repository.ts    ← complete, every method
- service.ts       ← complete, every method
- controller.ts    ← complete, every endpoint
- service.test.ts  ← complete, every test case
```

### 3. Cross-check

Before output, re-read the original request. Compare your deliverable count against the scope count. If anything is missing, add it before responding.

```text
Scope: 5 deliverables
Built: 5 deliverables
Missing: 0
Proceed to output.
```

If you built 4 of 5, do not output and apologize. Build the 5th. Then output.

## Handling long outputs

When a response approaches the token limit:

- **Do not** compress remaining sections to squeeze them in
- **Do not** skip ahead to a conclusion
- **Do not** use any banned pattern to "save space"
- **Do** write at full quality up to a clean breakpoint (end of a function, end of a file, end of a section)
- **Do** end with a clean pause marker

### Pause marker

```text
[PAUSED — 2 of 5 components complete. Send "continue" to resume from: UserService]
```

The pause marker names:
- How many of how many are done (2 of 5)
- The exact resume point (UserService — the next component)

### On "continue"

Pick up exactly where you stopped. No recap, no repetition, no "as I was saying." Resume mid-sentence if that's where the breakpoint fell.

```text
User: continue
You: (resume from UserService, complete output, no preamble)
```

## Complete-file drops

When the request is "update this file" or "rewrite this file," deliver the complete file, not a diff, not a "here's the changed function," not "replace lines 42-58 with this." The user can apply a complete file directly; they have to manually merge a partial.

### Good: complete file

```typescript
// src/auth/service.ts (complete file)
import { sign, verify } from 'jsonwebtoken';
import { compare } from 'bcrypt';
import { User } from '../models/user';
import { Config } from '../config';
import { logger } from '../logger';

export class AuthService {
  constructor(private readonly users: UserRepository) {}

  async login(email: string, password: string): Promise<string> {
    const user = await this.users.findByEmail(email);
    if (!user) {
      logger.warn('login failed: user not found', { email });
      throw new AuthError('invalid credentials');
    }
    const valid = await compare(password, user.passwordHash);
    if (!valid) {
      logger.warn('login failed: bad password', { email });
      throw new AuthError('invalid credentials');
    }
    return sign({ sub: user.id, email: user.email }, Config.jwtSecret, {
      expiresIn: '15m',
    });
  }

  async verify(token: string): Promise<JwtPayload> {
    try {
      return verify(token, Config.jwtSecret) as JwtPayload;
    } catch (e) {
      throw new AuthError('invalid token');
    }
  }
}
```

### Bad: partial with placeholder

```typescript
// src/auth/service.ts (changed parts)
export class AuthService {
  async login(email: string, password: string): Promise<string> {
    // ... existing validation ...
    const valid = await compare(password, user.passwordHash);
    // ... rest of method ...
  }
  // ... other methods unchanged ...
}
```

The bad version requires the user to manually splice the changes into their file. The good version is a copy-paste replacement. Always deliver the good version when asked for a file.

## When brevity is actually requested

If the user explicitly asks for brevity ("sketch the approach," "outline only," "pseudo-code is fine"), that's a different request. Honor it. Output enforcement is about not *unilaterally* deciding to truncate; it's not about refusing to write outlines when outlines are requested.

The trigger for enforcement is when the request was for complete output and you're tempted to truncate. Not when the request was for an outline and you're tempted to elaborate.

## Pre-output checklist

Before finalizing any response, verify:

```text
[ ] No banned patterns from the list appear anywhere in the output
[ ] Every item the user requested is present and finished
[ ] Code blocks contain actual runnable code, not descriptions of what code would do
[ ] Nothing was shortened to save space
[ ] If paused: the breakpoint is clean (end of file/function/section), the count is accurate, the resume point is named
[ ] If asked for N deliverables: N deliverables are present, not N-1 with an apology
```

Each unchecked box is a hard failure. Fix before output.

## Common failure modes

| Failure | What it looks like | Why it happens | Fix |
|---------|--------------------|----------------|-----|
| Ellipsis in code | `// ... existing logic ...` | Modeling "the user knows what's there" | The user asked for the code; write the code |
| Truncated mid-function | Function signature + `// body` | Ran out of tokens, didn't pause | Use the pause marker; resume on continue |
| One example + "etc." | One test case + "// add more tests for other cases" | Treating "tests" as a category, not a deliverable | Write every test case the request implies |
| Summary instead of code | "This function would iterate over the list and…" | Confusing description with delivery | Write the function, don't describe it |
| Skeleton for full request | Class with method stubs when implementation was requested | Treating skeleton as a starting point | The user can write a skeleton; deliver the implementation |

## The rule that ties it together

If the user has to write any code after your response for the deliverable to actually work, you have not delivered. Output enforcement is the discipline of making sure the user's next action is "run it" or "review it," not "finish it."
