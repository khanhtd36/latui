# ADF is the rich text model, both directions

Jira Cloud v3 stores descriptions, comments and notification snippets as Atlassian Document Format (ADF). We read ADF and render it to markdown for glamour, and we write comments as markdown converted to ADF and posted through v3. We own both converters.

## Considered Options

- Render `renderedFields` HTML via an HTML-to-markdown step, post comments as v2 wiki markup: rejected — HTML is messier to convert than structured ADF, and v2/wiki markup is legacy.
- Render ADF, post v2 wiki markup: rejected — two text formats to maintain instead of one.

## Consequences

Formatting markdown cannot express (merged-cell tables, panels, media) is shown degraded and is never written back. Issue description is therefore read-only in v1; rich edits go to the browser.
