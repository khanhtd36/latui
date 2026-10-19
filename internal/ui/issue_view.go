package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/jira"
)

// markdownCache renders markdown with glamour, once per text and width.
type markdownCache struct {
	renderers map[int]*glamour.TermRenderer
	out       map[string]string
}

func newMarkdownCache() *markdownCache {
	return &markdownCache{renderers: map[int]*glamour.TermRenderer{}, out: map[string]string{}}
}

func (c *markdownCache) render(md string, width int) []string {
	width = max(20, width)
	key := fmt.Sprintf("%d\x00%s", width, md)
	if s, ok := c.out[key]; ok {
		return strings.Split(s, "\n")
	}
	r, ok := c.renderers[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
		if err != nil {
			return wrap(md, width)
		}
		c.renderers[width] = r
	}
	s, err := r.Render(md)
	if err != nil {
		return wrap(md, width)
	}
	s = strings.Trim(s, "\n")
	c.out[key] = s
	return strings.Split(s, "\n")
}

// view draws Issue detail: the header, then fields beside the
// description (stacked on narrow screens), then the activity tabs.
func (d *issueModel) view(width, height int) []string {
	if d.issue == nil {
		if d.err != nil {
			return padLines(append([]string{styleTitle.Render(d.key)}, errorLines(d.err, width)...), height)
		}
		return padLines([]string{styleTitle.Render(d.key), styleYellow.Render("loading…")}, height)
	}
	header := d.headerLines(width)
	bodyH := max(6, height-len(header))
	var body []string
	if width >= 110 {
		topH := max(6, bodyH*2/5)
		fieldsW := min(52, width/3)
		top := framePanes(topH,
			d.pane(paneFields, "Fields", d.fieldLines(fieldsW-2), fieldsW, topH),
			d.pane(paneDescription, "Description", d.descriptionLines(width-fieldsW-2), width-fieldsW, topH),
		)
		bottom := framePanes(bodyH-topH, d.pane(paneActivity, "", d.activityLines(width-2), width, bodyH-topH))
		body = append(top, bottom...)
	} else {
		fieldsH := max(4, bodyH/3)
		descH := max(4, bodyH/4)
		body = append(body, framePanes(fieldsH, d.pane(paneFields, "Fields", d.fieldLines(width-2), width, fieldsH))...)
		body = append(body, framePanes(descH, d.pane(paneDescription, "Description", d.descriptionLines(width-2), width, descH))...)
		actH := bodyH - fieldsH - descH
		body = append(body, framePanes(actH, d.pane(paneActivity, "", d.activityLines(width-2), width, actH))...)
	}
	return padLines(append(header, body...), height)
}

func (d *issueModel) headerLines(width int) []string {
	f := d.issue.Fields
	head := styleCyan.Bold(true).Render(d.key) + "  " + styleTitle.Render(f.Summary)
	meta := []string{
		statusStyle(f.Status.Category.Key).Bold(true).Render(strings.ToUpper(f.Status.Name)),
		f.IssueType.Name,
	}
	if f.Priority != nil {
		meta = append(meta, f.Priority.Name)
	}
	if f.Project.Name != "" {
		meta = append(meta, f.Project.Name)
	}
	line2 := strings.Join(meta, styleDim.Render(" · "))
	if d.saving != "" {
		line2 += styleYellow.Render("  saving " + d.saving + "…")
	}
	if d.refreshErr != nil {
		line2 += styleRed.Render("  refresh failed: " + d.refreshErr.Error())
	}
	if len(d.back) > 0 {
		line2 += styleDim.Render("  (esc back to " + d.back[len(d.back)-1] + ")")
	}
	return []string{truncate(head, width), truncate(line2, width)}
}

// pane frames one of the panes, scrolled, and focused when it has the keys.
func (d *issueModel) pane(which int, title string, lines []string, width, height int) pane {
	inner := paneInnerHeight(height)
	if which == paneActivity {
		labels := make([]string, tabCount)
		for i, name := range tabNames {
			labels[i] = fmt.Sprintf("%d %s%s", i+1, name, d.tabCount(i))
		}
		bar := tabBar(labels, d.tab, width-2)
		lines = append([]string{bar, ""}, lines...)
		if d.tab == tabLinks || d.tab == tabAttachments {
			// Keep the cursor's row in view.
			at := d.cursor[d.tab] + 2
			if at < d.scroll[which]+2 {
				d.scroll[which] = max(0, at-2)
			}
			if at >= d.scroll[which]+inner {
				d.scroll[which] = at - inner + 1
			}
		}
	}
	maxScroll := max(0, len(lines)-inner)
	d.scroll[which] = min(d.scroll[which], maxScroll)
	shown := lines
	if which == paneActivity && d.scroll[which] > 0 {
		// The tab bar stays put.
		shown = append([]string{lines[0], lines[1]}, lines[min(len(lines), 2+d.scroll[which]):]...)
	} else if d.scroll[which] > 0 {
		shown = lines[d.scroll[which]:]
	}
	pos := ""
	if maxScroll > 0 {
		pos = fmt.Sprintf("%d%%", 100*d.scroll[which]/maxScroll)
	}
	return pane{title: title, width: width, lines: shown, focused: d.focus == which, pos: pos}
}

func (d *issueModel) tabCount(tab int) string {
	n := 0
	switch tab {
	case tabComments:
		n = len(d.comments)
	case tabHistory:
		n = len(d.history)
	case tabLinks:
		n = len(d.linkRows())
	case tabAttachments:
		n = len(d.issue.Fields.Attachments)
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d)", n)
}

func userName(u *jira.User) string {
	if u == nil || u.DisplayName == "" {
		return styleDim.Render("Unassigned")
	}
	return u.DisplayName
}

func names(ns []string) string {
	if len(ns) == 0 {
		return styleDim.Render("None")
	}
	return strings.Join(ns, ", ")
}

func (d *issueModel) fieldLines(width int) []string {
	f := d.issue.Fields
	const labelW = 13
	var lines []string
	add := func(label, value string) {
		if value == "" {
			return
		}
		wrapped := wrap(value, max(8, width-labelW-1))
		for i, w := range wrapped {
			l := strings.Repeat(" ", labelW)
			if i == 0 {
				l = styleDim.Render(fit(label, labelW))
			}
			lines = append(lines, l+" "+w)
		}
	}
	add("Status", statusStyle(f.Status.Category.Key).Render(f.Status.Name))
	if f.Resolution != nil {
		add("Resolution", f.Resolution.Name)
	}
	add("Assignee", userName(f.Assignee))
	add("Reporter", userName(f.Reporter))
	if f.Priority != nil {
		add("Priority", f.Priority.Name)
	}
	add("Type", f.IssueType.Name)
	add("Labels", names(f.Labels))
	comps := make([]string, 0, len(f.Components))
	for _, c := range f.Components {
		comps = append(comps, c.Name)
	}
	add("Components", names(comps))
	if len(f.FixVersions) > 0 {
		vs := make([]string, 0, len(f.FixVersions))
		for _, v := range f.FixVersions {
			vs = append(vs, v.Name)
		}
		add("Fix versions", strings.Join(vs, ", "))
	}
	if f.Parent != nil {
		add("Parent", f.Parent.Key+" "+f.Parent.Fields.Summary)
	}
	add("Due", f.Due)
	add("Created", dateTime(f.Created))
	add("Updated", dateTime(f.Updated))
	if cf := d.issue.CustomFields(); len(cf) > 0 {
		lines = append(lines, "", styleHeader.Render("Other fields"))
		for _, c := range cf {
			add(c.Name, c.Value)
		}
	}
	return lines
}

// dateTime shows a Jira timestamp in local time with how long ago.
func dateTime(s string) string {
	t := jira.Time(s)
	if t.IsZero() {
		return s
	}
	s = t.Local().Format("2006-01-02 15:04")
	if time.Since(t) < 7*24*time.Hour {
		s += styleDim.Render(" (" + relTime(t) + ")")
	}
	return s
}

func (d *issueModel) descriptionLines(width int) []string {
	md := d.issue.Description()
	if strings.TrimSpace(md) == "" {
		return []string{styleDim.Render("No description.")}
	}
	return d.md.render(md, width-1)
}

func (d *issueModel) activityLines(width int) []string {
	switch d.tab {
	case tabComments:
		return d.commentLines(width)
	case tabHistory:
		return d.historyLines(width)
	case tabLinks:
		return d.linkLines(width)
	case tabAttachments:
		return d.attachmentLines(width)
	}
	return nil
}

func (d *issueModel) commentLines(width int) []string {
	switch {
	case d.cErr != nil:
		return errorLines(d.cErr, width)
	case !d.cLoaded:
		return []string{styleYellow.Render("loading comments…")}
	case len(d.comments) == 0:
		return []string{styleDim.Render("No comments yet. Press c to add one.")}
	}
	var lines []string
	for i, c := range d.comments {
		if i > 0 {
			lines = append(lines, styleDim.Render(strings.Repeat("─", max(1, width-2))))
		}
		head := styleTitle.Render(c.Author.DisplayName) + styleDim.Render(" · "+dateTime(c.Created))
		if c.Updated != "" && c.Updated != c.Created {
			head += styleDim.Render(" · edited")
		}
		lines = append(lines, truncate(head, width))
		lines = append(lines, d.md.render(c.Markdown(), width-1)...)
	}
	return lines
}

func (d *issueModel) historyLines(width int) []string {
	switch {
	case d.hErr != nil:
		return errorLines(d.hErr, width)
	case !d.hLoaded:
		return []string{styleYellow.Render("loading history…")}
	case len(d.history) == 0:
		return []string{styleDim.Render("No changes yet.")}
	}
	var lines []string
	for _, h := range d.history {
		lines = append(lines, truncate(styleTitle.Render(cmpOr(h.Author.DisplayName, "Automation"))+styleDim.Render(" · "+dateTime(h.Created)), width))
		for _, c := range h.Changes {
			from, to := oneLine(c.From), oneLine(c.To)
			if from == "" {
				from = "∅"
			}
			if to == "" {
				to = "∅"
			}
			lines = append(lines, truncate("  "+styleCyan.Render(c.Field)+": "+styleDim.Render(from)+" → "+to, width))
		}
		lines = append(lines, "")
	}
	return lines
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (d *issueModel) linkLines(width int) []string {
	rows := d.linkRows()
	if len(rows) == 0 {
		return []string{styleDim.Render("No parent, subtasks or links.")}
	}
	relW := 4
	for _, r := range rows {
		relW = max(relW, ansi.StringWidth(r.relation))
	}
	relW = min(relW, 24)
	lines := make([]string, 0, len(rows))
	for i, r := range rows {
		prefix := "  "
		if i == d.cursor[tabLinks] && d.focus == paneActivity {
			prefix = styleCursor.Render("▌ ")
		}
		line := prefix + styleDim.Render(fit(r.relation, relW)) + "  " + styleCyan.Render(r.key) + "  " +
			statusStyle(r.category).Render(r.status) + "  " + r.summary
		lines = append(lines, truncate(line, width))
	}
	return append(lines, "", styleDim.Render("enter open here · o open in the browser"))
}

func (d *issueModel) attachmentLines(width int) []string {
	as := d.issue.Fields.Attachments
	if len(as) == 0 {
		return []string{styleDim.Render("No attachments.")}
	}
	lines := make([]string, 0, len(as))
	for i, a := range as {
		prefix := "  "
		if i == d.cursor[tabAttachments] && d.focus == paneActivity {
			prefix = styleCursor.Render("▌ ")
		}
		line := prefix + a.Filename + styleDim.Render("  "+size(a.Size)+" · "+a.Author.DisplayName+" · "+relTime(jira.Time(a.Created)))
		lines = append(lines, truncate(line, width))
	}
	return append(lines, "", styleDim.Render("o open in the browser"))
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
