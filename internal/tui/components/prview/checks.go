package prview

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	ghchecks "github.com/dlvhdr/x/gh-checks"
)

type checkSectionStatus int

const (
	statusSuccess checkSectionStatus = iota
	statusFailure
	statusWaiting
	statusNonRequested
)

func (m *Model) renderChecksOverview() string {
	w := m.getIndentedContentWidth()

	if m.pr.Data.Primary.State == "MERGED" {
		return m.viewMergedStatus()
	}

	if m.pr.Data.Primary.State == "CLOSED" {
		return m.viewClosedStatus()
	}

	review, rStatus := m.viewReviewStatus()
	checks, cStatus := m.viewChecksStatus()
	merge, mStatus := m.viewMergeStatus()

	borderColor := m.ctx.Theme.FaintBorder
	if rStatus == statusFailure || cStatus == statusFailure || mStatus == statusFailure {
		borderColor = m.ctx.Theme.ErrorText
	} else if rStatus == statusSuccess && cStatus == statusSuccess && mStatus == statusSuccess {
		borderColor = m.ctx.Theme.SuccessText
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(w)
	parts := make([]string, 0)
	if review != "" {
		parts = append(parts, review)
	}
	if checks != "" {
		parts = append(parts, checks)
	}
	if merge != "" {
		parts = append(parts, merge)
	}

	return box.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m *Model) viewChecksStatus() (string, checkSectionStatus) {
	checks := ""

	if !m.pr.Data.IsEnriched {
		return m.viewCheckCategory(
			m.ctx.Styles.Common.WaitingGlyph,
			"Loading...",
			"",
			false,
		), statusWaiting
	}

	stats := m.getChecksStats()
	var icon, title string
	var status checkSectionStatus

	statStrs := make([]string, 0)
	if stats.failed > 0 {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Some checks were not successful"
		status = statusFailure
	} else if stats.awaitingApproval > 0 {
		icon = m.ctx.Styles.Common.ActionRequiredGlyph
		title = "Workflows awaiting approval"
		status = statusWaiting
	} else if stats.inProgress > 0 {
		icon = m.ctx.Styles.Common.WaitingGlyph
		title = "Some checks haven’t completed yet"
		status = statusWaiting
	} else if stats.succeeded > 0 {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "All checks have passed"
		status = statusSuccess
	} else {
		return "", statusWaiting
	}

	if stats.failed > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d failing", stats.failed))
	}
	if stats.awaitingApproval > 0 {
		statStrs = append(
			statStrs,
			fmt.Sprintf(
				"%d awaiting approval. Press %s to run.",
				stats.awaitingApproval,
				m.ctx.Styles.KeyHint.Render(keys.PRKeys.ApproveWorkflows.Keys()[0])),
		)
	}
	if stats.inProgress > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d in progress", stats.inProgress))
	}
	if stats.skipped > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d skipped", stats.skipped))
	}
	if stats.neutral > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d neutral", stats.neutral))
	}
	if stats.succeeded > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d successful", stats.succeeded))
	}
	if title != "" {
		checksBar := m.viewChecksBar()
		checkDetails := m.viewCheckRunSummary()
		checksBottom := lipgloss.JoinVertical(
			lipgloss.Left,
			strings.Join(statStrs, ", "),
			checksBar,
			checkDetails,
		)
		checks = m.viewCheckCategory(icon, title, checksBottom, false)
	}
	return checks, status
}

func (m *Model) viewCheckRunSummary() string {
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return ""
	}

	type item struct {
		name   string
		detail string
		icon   string
		number int
	}
	groups := []struct {
		title string
		items []item
	}{
		{title: "Failing checks"},
		{title: "Skipped checks"},
		{title: "In progress checks"},
		{title: "Successful checks"},
		{title: "Other checks"},
	}
	lastCommit := commits[0].Commit
	checkNumber := 0
	for _, node := range lastCommit.StatusCheckRollup.Contexts.Nodes {
		switch node.Typename {
		case "CheckRun":
			run := node.CheckRun
			checkNumber++
			name := renderCheckRunName(run)
			state := string(run.Conclusion)
			if ghchecks.IsStatusWaiting(string(run.Status)) {
				elapsed := ""
				if !run.StartedAt.IsZero() {
					elapsed = formatCheckElapsed(time.Since(run.StartedAt))
				}
				detail := "This check has started..."
				if elapsed != "" {
					detail = "Started " + elapsed + " ago — " + detail
				}
				groups[2].items = append(
					groups[2].items,
					item{name, detail, m.ctx.Styles.Common.WaitingGlyph, checkNumber},
				)
			} else {
				duration := ""
				if !run.StartedAt.IsZero() && !run.CompletedAt.IsZero() {
					duration = formatCheckElapsed(run.CompletedAt.Sub(run.StartedAt))
				}
				switch {
				case ghchecks.IsConclusionAFailure(state):
					detail := "Failing"
					if duration != "" {
						detail += " after " + duration
					}
					groups[0].items = append(groups[0].items, item{name, detail, m.ctx.Styles.Common.FailureGlyph, checkNumber})
				case ghchecks.IsConclusionASkip(state):
					groups[1].items = append(groups[1].items, item{name, "Skipped", m.ctx.Styles.Common.WaitingGlyph, checkNumber})
				case ghchecks.IsConclusionASuccess(state):
					detail := "Successful"
					if duration != "" {
						detail += " in " + duration
					}
					groups[3].items = append(groups[3].items, item{name, detail, m.ctx.Styles.Common.SuccessGlyph, checkNumber})
				default:
					groups[4].items = append(groups[4].items, item{name, state, m.ctx.Styles.Common.WaitingGlyph, checkNumber})
				}
			}
		case "StatusContext":
			ctx := node.StatusContext
			category, icon := m.renderStatusContextConclusion(ctx)
			group := 3
			switch category {
			case CheckFailure:
				group = 0
			case CheckWaiting:
				group = 2
			}
			groups[group].items = append(
				groups[group].items,
				item{
					name:   renderStatusContextName(ctx),
					detail: strings.TrimSpace(string(ctx.Description)),
					icon:   icon,
				},
			)
		}
	}

	parts := make([]string, 0)
	for _, group := range groups {
		if len(group.items) == 0 {
			continue
		}
		parts = append(parts, lipgloss.NewStyle().Bold(true).Render(group.title))
		for _, entry := range group.items {
			name := entry.name
			if entry.number > 0 {
				name += lipgloss.NewStyle().
					Foreground(m.ctx.Theme.FaintText).
					Render(fmt.Sprintf(" [%d]", entry.number))
			}
			line := lipgloss.JoinHorizontal(lipgloss.Top, entry.icon, " ", name)
			if entry.detail != "" {
				line = lipgloss.JoinVertical(
					lipgloss.Left,
					line,
					lipgloss.NewStyle().
						Foreground(m.ctx.Theme.FaintText).
						MarginLeft(2).
						Render(entry.detail),
				)
			}
			parts = append(parts, lipgloss.NewStyle().MarginLeft(2).Render(line))
		}
	}
	if checkNumber > 0 {
		hint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).
			Render("Press a number to view logs; q returns to the TUI")
		parts = append(parts, hint)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) checkRunURL(number int) string {
	run, ok := m.checkRun(number)
	if !ok {
		return ""
	}
	return string(run.DetailsUrl)
}

func (m *Model) checkRun(number int) (data.CheckRun, bool) {
	if number < 1 || number > 9 || m.pr == nil || m.pr.Data == nil || m.pr.Data.Primary == nil {
		return data.CheckRun{}, false
	}
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return data.CheckRun{}, false
	}
	current := 0
	for _, node := range commits[0].Commit.StatusCheckRollup.Contexts.Nodes {
		if node.Typename != "CheckRun" {
			continue
		}
		current++
		if current == number {
			return node.CheckRun, true
		}
	}
	return data.CheckRun{}, false
}

func checkRunLogArgs(run data.CheckRun, repo string) []string {
	jobID := run.DatabaseId.String()
	workflowName := strings.TrimSpace(string(run.CheckSuite.WorkflowRun.Workflow.Name))
	if jobID == "" || jobID == "0" || workflowName == "" || repo == "" {
		return nil
	}
	logFlag := "--log"
	if ghchecks.IsConclusionAFailure(string(run.Conclusion)) {
		logFlag = "--log-failed"
	}
	return []string{
		"run", "view", logFlag, "--job", jobID, "-R", repo,
	}
}

func formatCheckElapsed(duration time.Duration) string {
	if duration < time.Minute {
		seconds := int(math.Round(duration.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%ds", seconds)
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(math.Round(duration.Minutes())))
	}
	return fmt.Sprintf("%dh", int(math.Round(duration.Hours())))
}

func (m *Model) viewMergeStatus() (string, checkSectionStatus) {
	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()
	if m.pr.Data.Primary.MergeStateStatus == "CLEAN" ||
		m.pr.Data.Primary.MergeStateStatus == "UNSTABLE" {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "No conflicts with base branch"
		subtitle = "Changes can be cleanly merged"
		status = statusSuccess
	} else if m.pr.Data.Primary.IsDraft {
		icon = m.ctx.Styles.Common.DraftGlyph
		title = "This pull request is still a work in progress"
		subtitle = "Draft pull requests cannot be merged"
		status = statusWaiting
	} else if m.pr.Data.Primary.MergeStateStatus == "BLOCKED" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Merging is blocked"
		if numReviewOwners > 0 {
			subtitle = "Waiting on code owner review"
		}
		status = statusFailure
	} else if m.pr.Data.Primary.Mergeable == "CONFLICTING" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "This branch has conflicts that must be resolved"
		status = statusFailure
		if m.pr.Data.Primary.MergeStateStatus == "CLEAN" {
			subtitle = "Changes can be cleanly merged"
		}
	}
	return m.viewCheckCategory(icon, title, subtitle, true), status
}

func (m *Model) viewMergedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Styles.Colors.MergedPR).
		Width(w)
	return box.Render(m.viewCheckCategory(
		m.ctx.Styles.Common.MergedGlyph,
		"Pull request successfully merged and closed",
		"The branch has been merged",
		true,
	))
}

func (m *Model) viewClosedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w)
	return box.Render(m.viewCheckCategory(
		"",
		"Closed with unmerged commits",
		"This pull request is closed",
		true,
	))
}

func (m *Model) viewReviewStatus() (string, checkSectionStatus) {
	pr := m.pr
	if pr.Data == nil {
		return "", statusWaiting
	}

	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()

	numApproving, numChangesRequested, numPending, numCommented := 0, 0, 0, 0

	for _, node := range pr.Data.Enriched.Reviews.Nodes {
		switch node.State {
		case "APPROVED":
			numApproving++
		case "CHANGES_REQUESTED":
			numChangesRequested++
		case "PENDING":
			numPending++
		case "COMMENTED":
			numCommented++
		}
	}

	switch pr.Data.Primary.ReviewDecision {
	case "APPROVED":
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "Changes approved"
		subtitle = fmt.Sprintf("%d approving reviews", numApproving)
		status = statusSuccess
	case "CHANGES_REQUESTED":
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Changes requested"
		subtitle = fmt.Sprintf("%d requested changes", numChangesRequested)
		status = statusFailure
	case "REVIEW_REQUIRED":
		icon = pr.Ctx.Styles.Common.WaitingGlyph
		title = "Review Required"

		branchRules := m.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
		if len(branchRules) > 0 && branchRules[0].RequiresCodeOwnerReviews && numApproving < 1 {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if numApproving < numReviewOwners {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if len(branchRules) > 0 && numApproving <
			branchRules[0].RequiredApprovingReviewCount {
			subtitle = fmt.Sprintf("Need %d more approval",
				branchRules[0].RequiredApprovingReviewCount-numApproving)
			status = statusWaiting
		} else if numCommented > 0 {
			subtitle = fmt.Sprintf("%d reviewers left comments", numCommented)
			status = statusWaiting
		}
	default:
		icon = pr.Ctx.Styles.Common.PersonGlyph
		title = "Reviews"
		subtitle = "None requested"
		status = statusNonRequested
	}

	return m.viewCheckCategory(icon, title, subtitle, false), status
}

func (m *Model) viewCheckCategory(icon, title, subtitle string, isLast bool) string {
	w := m.getIndentedContentWidth() - 2
	part := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, !isLast, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w).
		Padding(1)

	sTitle := lipgloss.NewStyle().Bold(true)
	sSub := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	category := lipgloss.JoinHorizontal(lipgloss.Top, icon, " ", sTitle.Render(title))

	if subtitle != "" {
		category = lipgloss.JoinVertical(
			lipgloss.Left,
			category,
			sSub.MarginLeft(2).Render(subtitle),
		)
	}
	if category == "" {
		return ""
	}
	return part.Render(category)
}

func (m *Model) viewChecksBar() string {
	w := m.getIndentedContentWidth() - 6
	stats := m.getChecksStats()
	total := float64(
		stats.failed + stats.skipped + stats.neutral + stats.succeeded + stats.inProgress + stats.awaitingApproval,
	)
	numSections := 0
	if stats.failed > 0 {
		numSections++
	}
	if stats.awaitingApproval > 0 {
		numSections++
	}
	if stats.inProgress > 0 {
		numSections++
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		numSections++
	}
	if stats.succeeded > 0 {
		numSections++
	}
	// subtract number of spacers
	w -= numSections - 1
	if w < 0 {
		w = 0
	}

	sections := make([]string, 0)
	if stats.failed > 0 {
		failWidth := int(math.Floor((float64(stats.failed) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(failWidth).Foreground(
			m.ctx.Theme.ErrorText).Height(1).Render(strings.Repeat("▃", failWidth)))
	}
	if stats.awaitingApproval > 0 {
		awWidth := int(math.Floor((float64(stats.awaitingApproval) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(awWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", awWidth)))
	}
	if stats.inProgress > 0 {
		ipWidth := int(math.Floor((float64(stats.inProgress) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(ipWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", ipWidth)))
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		skipWidth := int(math.Floor((float64(stats.skipped+stats.neutral) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(skipWidth).Foreground(
			m.ctx.Theme.FaintText).Height(1).Render(strings.Repeat("▃", skipWidth)))
	}
	if stats.succeeded > 0 {
		succWidth := int(math.Floor((float64(stats.succeeded) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(succWidth).Foreground(
			m.ctx.Theme.SuccessText).Height(1).Render(strings.Repeat("▃", succWidth)))
	}

	return strings.Join(sections, " ")
}

func renderCheckRunName(checkRun data.CheckRun) string {
	var parts []string
	creator := strings.TrimSpace(string(checkRun.CheckSuite.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	workflow := strings.TrimSpace(string(checkRun.CheckSuite.WorkflowRun.Workflow.Name))
	if workflow != "" {
		parts = append(parts, workflow)
	}

	name := strings.TrimSpace(string(checkRun.Name))
	if name != "" {
		parts = append(parts, name)
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

type CheckCategory int

const (
	CheckWaiting CheckCategory = iota
	CheckFailure
	CheckSuccess
)

func (m *Model) renderCheckRunConclusion(checkRun data.CheckRun) (CheckCategory, string) {
	if ghchecks.IsStatusWaiting(string(checkRun.Status)) {
		return CheckWaiting, m.ctx.Styles.Common.WaitingGlyph
	}

	if ghchecks.IsConclusionAFailure(string(checkRun.Conclusion)) {
		return CheckFailure, m.ctx.Styles.Common.FailureGlyph
	}

	return CheckSuccess, m.ctx.Styles.Common.SuccessGlyph
}

func (m *Model) renderStatusContextConclusion(
	statusContext data.StatusContext,
) (CheckCategory, string) {
	conclusionStr := string(statusContext.State)
	if ghchecks.IsStatusWaiting(conclusionStr) {
		return CheckWaiting, m.ctx.Styles.Common.WaitingGlyph
	}

	if ghchecks.IsConclusionAFailure(conclusionStr) {
		return CheckFailure, m.ctx.Styles.Common.FailureGlyph
	}

	return CheckSuccess, m.ctx.Styles.Common.SuccessGlyph
}

func renderStatusContextName(statusContext data.StatusContext) string {
	var parts []string
	creator := strings.TrimSpace(string(statusContext.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	context := strings.TrimSpace(string(statusContext.Context))
	if context != "" && context != "/" {
		parts = append(parts, context)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

func (sidebar *Model) renderChecks() string {
	title := sidebar.ctx.Styles.Common.MainTextStyle.MarginBottom(1).
		Underline(true).
		Render(" All Checks")

	commits := sidebar.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"Loading...",
		)
	}

	failures := make([]string, 0)
	waiting := make([]string, 0)
	rest := make([]string, 0)
	awaitingApproval := make([]string, 0)
	pending := make([]string, 0)

	lastCommit := commits[0]

	// Workflows awaiting approval or still queued don't have CheckRun objects,
	// so show those suites separately. Running and completed checks are rendered
	// individually from StatusCheckRollup below.
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		workflowName := strings.TrimSpace(string(suite.WorkflowRun.Workflow.Name))
		if workflowName == "" {
			workflowName = strings.TrimSpace(string(suite.App.Name))
		}
		if workflowName == "" {
			workflowName = "Workflow"
		}

		if suite.Conclusion == "ACTION_REQUIRED" {
			check := lipgloss.JoinHorizontal(
				lipgloss.Top,
				sidebar.ctx.Styles.Common.ActionRequiredGlyph,
				" ",
				workflowName,
			)
			awaitingApproval = append(awaitingApproval, check)
		} else if suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING" {
			check := lipgloss.JoinHorizontal(
				lipgloss.Top,
				sidebar.ctx.Styles.Common.WaitingGlyph,
				" ",
				workflowName,
			)
			pending = append(pending, check)
		}
	}

	// Build a set of reported check names to compare against required checks
	reportedChecks := make(map[string]bool)

	for _, node := range lastCommit.Commit.StatusCheckRollup.Contexts.Nodes {
		var category CheckCategory
		var check string
		var checkName string
		switch node.Typename {
		case "CheckRun":
			checkRun := node.CheckRun
			var renderedStatus string
			category, renderedStatus = sidebar.renderCheckRunConclusion(checkRun)
			checkName = string(checkRun.Name)
			name := renderCheckRunName(checkRun)
			check = lipgloss.JoinHorizontal(lipgloss.Top, renderedStatus, " ", name)
		case "StatusContext":
			statusContext := node.StatusContext
			var status string
			category, status = sidebar.renderStatusContextConclusion(statusContext)
			checkName = string(statusContext.Context)
			check = lipgloss.JoinHorizontal(
				lipgloss.Top,
				status,
				" ",
				renderStatusContextName(statusContext),
			)
		}

		reportedChecks[checkName] = true

		switch category {
		case CheckWaiting:
			waiting = append(waiting, check)
		case CheckFailure:
			failures = append(failures, check)
		default:
			rest = append(rest, check)
		}
	}

	// Check for required status checks that haven't been reported yet
	branchRules := sidebar.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
	if len(branchRules) > 0 {
		for _, requiredContext := range branchRules[0].RequiredStatusCheckContexts {
			contextName := string(requiredContext)
			if !reportedChecks[contextName] {
				// Required check hasn't been reported yet
				check := lipgloss.JoinHorizontal(
					lipgloss.Top,
					sidebar.ctx.Styles.Common.WaitingGlyph,
					" ",
					contextName,
				)
				pending = append(pending, check)
			}
		}
	}

	if len(awaitingApproval)+len(pending)+len(waiting)+len(failures)+len(rest) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			lipgloss.NewStyle().
				Italic(true).
				PaddingLeft(2).
				Width(sidebar.getIndentedContentWidth()).
				Render("No checks to display..."),
		)
	}

	parts := make([]string, 0)

	// Show awaiting approval workflows first
	if len(awaitingApproval) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(sidebar.ctx.Theme.WarningText).
			Render(fmt.Sprintf("Awaiting Approval (%d)", len(awaitingApproval)))
		parts = append(parts, sectionHeader)
		parts = append(parts, awaitingApproval...)
		parts = append(parts, "") // spacing
	}

	// Show pending workflows
	if len(pending) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(sidebar.ctx.Theme.WarningText).
			Render(fmt.Sprintf("Pending (%d)", len(pending)))
		parts = append(parts, sectionHeader)
		parts = append(parts, pending...)
		parts = append(parts, "") // spacing
	}

	parts = append(parts, failures...)
	if len(waiting) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(sidebar.ctx.Theme.WarningText).
			Render(fmt.Sprintf("In Progress Checks (%d)", len(waiting)))
		parts = append(parts, sectionHeader)
		parts = append(parts, waiting...)
	}
	parts = append(parts, rest...)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		lipgloss.NewStyle().PaddingLeft(2).Width(sidebar.getIndentedContentWidth()).Render(
			lipgloss.JoinVertical(lipgloss.Left, parts...)),
	)
}

type checksStats struct {
	succeeded        int
	neutral          int
	failed           int
	skipped          int
	inProgress       int
	awaitingApproval int
}

func (m *Model) getStatusCheckRollupStats(rollup data.StatusCheckRollupStats) checksStats {
	var res checksStats
	allChecks := make([]data.ContextCountByState, 0)
	allChecks = append(allChecks, rollup.Contexts.CheckRunCountsByState...)
	allChecks = append(allChecks, rollup.Contexts.StatusContextCountsByState...)

	for _, count := range allChecks {
		state := string(count.State)
		if ghchecks.IsStatusWaiting(state) {
			res.inProgress += int(count.Count)
		} else if ghchecks.IsConclusionAFailure(state) {
			res.failed += int(count.Count)
		} else if ghchecks.IsConclusionASkip(state) {
			res.skipped += int(count.Count)
		} else if ghchecks.IsConclusionNeutral(state) {
			res.neutral += int(count.Count)
		} else if ghchecks.IsConclusionASuccess(state) {
			res.succeeded += int(count.Count)
		}
	}

	return res
}

func (m *Model) getChecksStats() checksStats {
	var res checksStats
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return res
	}

	lastCommit := commits[0]
	allChecks := make([]data.ContextCountByState, 0)
	allChecks = append(
		allChecks,
		lastCommit.Commit.StatusCheckRollup.Contexts.CheckRunCountsByState...)
	allChecks = append(
		allChecks,
		lastCommit.Commit.StatusCheckRollup.Contexts.StatusContextCountsByState...)

	for _, count := range allChecks {
		state := string(count.State)
		if ghchecks.IsStatusWaiting(state) {
			res.inProgress += int(count.Count)
		} else if ghchecks.IsConclusionAFailure(state) {
			res.failed += int(count.Count)
		} else if ghchecks.IsConclusionASkip(state) {
			res.skipped += int(count.Count)
		} else if ghchecks.IsConclusionNeutral(state) {
			res.neutral += int(count.Count)
		} else if ghchecks.IsConclusionASuccess(state) {
			res.succeeded += int(count.Count)
		}
	}

	// Count check suites that don't appear in statusCheckRollup
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		if suite.Conclusion == "ACTION_REQUIRED" {
			res.awaitingApproval++
		} else if suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING" {
			res.inProgress++
		}
	}

	return res
}

func (m *Model) numRequestedReviewOwners() int {
	numOwners := 0

	for _, node := range m.pr.Data.Enriched.ReviewRequests.Nodes {
		if node.AsCodeOwner {
			numOwners++
		}
	}

	return numOwners
}
