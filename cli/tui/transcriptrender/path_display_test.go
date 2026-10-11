package transcriptrender

import (
	"path/filepath"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestStructuredNoticePathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "project", "feature")
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKTREE_MODE
	notice := &transcriptpb.NoticeRow{
		Reason:      transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
		MessageType: &messageType,
		Worktree: &transcriptpb.WorktreeContext{
			Branch: textutil.Value("feature"), EffectiveCwd: path,
		},
	}
	for _, mode := range []Mode{ModeOngoing, ModeDetailCollapsed, ModeDetailExpanded} {
		_, got := noticeRoleAndText(notice, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, mode)
		_, want := noticeRoleAndText(&transcriptpb.NoticeRow{
			Reason: notice.Reason, MessageType: notice.MessageType,
			Worktree: &transcriptpb.WorktreeContext{
				Branch: notice.Worktree.Branch, EffectiveCwd: "~/project/feature",
			},
		}, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, mode)
		if got != want {
			t.Fatalf("mode %v path display = %q, want %q", mode, got, want)
		}
	}
	if notice.Worktree.EffectiveCwd != path {
		t.Fatal("display changed canonical worktree path")
	}
}

func TestNoticeSourcePathDisplayPreservesContentAndDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "AGENTS.md")
	notice := &transcriptpb.NoticeRow{SourcePath: &path}
	for _, mode := range []Mode{ModeOngoing, ModeDetailCollapsed, ModeDetailExpanded} {
		_, got := noticeRoleAndText(notice, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, mode)
		if got != "~/AGENTS.md" {
			t.Fatalf("mode %v source path = %q", mode, got)
		}
	}
	notice.CompactLabel = &path
	_, got := noticeRoleAndText(notice, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, ModeOngoing)
	if got != path {
		t.Fatalf("arbitrary compact label changed: %q", got)
	}
	notice.Diagnostic = &transcriptpb.Diagnostic{Detail: path}
	_, got = noticeRoleAndText(notice, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, ModeDetailExpanded)
	if got != path || *notice.SourcePath != path {
		t.Fatalf("canonical source or diagnostic changed: %q", got)
	}
}

func TestImageStructuredPathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "images", "photo.png")
	meta := toolMeta{ToolCallMeta: transcript.ToolCallMeta{
		ToolName: "view_image",
		RenderHint: &transcript.ToolRenderHint{
			Kind: transcript.ToolRenderKindPlain, Path: path,
		},
	}}
	got, ok := viewImageDisplayText(meta)
	if !ok || got != viewImageDisplayPrefix+"~/images/photo.png" {
		t.Fatalf("image path display = %q (%v)", got, ok)
	}
	if meta.RenderHint.Path != path {
		t.Fatal("display changed canonical image path")
	}
}
