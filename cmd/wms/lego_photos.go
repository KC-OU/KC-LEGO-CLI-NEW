package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// A set's own photos -- the box, the built model, a damaged part -- kept
// against the set directly, independent of any particular check (compare
// `wms lego attach`, which files a check's own proof). Reuses the same
// verified-upload path (VerifyProofFile/StoreProof): a genuine jpg, png or
// PDF, checked by its actual bytes, stored next to lego.db (or
// LEGO_ATTACHMENTS_DIR) alongside check proofs.

func newLegoSetPhotoCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:     "set-photo <set> <path>",
		Short:   "Attach a photo to a set — the box, the built model, anything worth keeping a picture of",
		Long:    "Verifies the file is a genuine, readable jpg, png or PDF (checking the actual bytes, not just the\nfile extension), then stores a copy and records it against the set — any number of photos, not just one.",
		Example: "  wms lego set-photo 75192-1 ./photos/falcon-box.jpg\n  wms lego set-photo 75192-1 ./photos/falcon-built.jpg --note \"finished build, Oct 2026\"",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			src := args[1]
			kind, err := lego.VerifyProofFile(src)
			if err != nil {
				return usageError("%s", err)
			}
			dst, err := lego.StoreProof(legoAttachmentsDir(), num, kind, src)
			if err != nil {
				return err
			}
			by := cliActor()
			p, err := db.AttachSetPhoto(num, kind, dst, note, by)
			if err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Attached %s photo #%d for %s", kind, p.ID, num)))
			say(ui.Fact(t, "Stored", dst))
			return emit(map[string]any{"set": num, "photo_id": p.ID, "kind": kind, "file": dst})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "a short note to keep with the photo")
	return cmd
}

func newLegoSetPhotosCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set-photos <set>",
		Short:   "List a set's attached photos",
		Example: "  wms lego set-photos 75192-1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			photos, err := db.SetPhotos(num)
			if err != nil {
				return err
			}
			if len(photos) == 0 {
				say(ui.Status(t, false, fmt.Sprintf("No photos attached to %s yet — wms lego set-photo %s <file>", num, num)))
				return emit(map[string]any{"set": num, "photos": photos})
			}
			rows := make([][]string, len(photos))
			for i, p := range photos {
				rows[i] = []string{strconv.FormatInt(p.ID, 10), p.Kind, p.FilePath, orDash(p.Note), p.By, p.UploadedAt.Local().Format("2 Jan 2006 15:04")}
			}
			say(ui.RenderColumns(t, []string{"ID", "Kind", "File", "Note", "By", "Uploaded"}, rows, fmt.Sprintf("%d photo(s) for %s", len(photos), num)))
			return emit(map[string]any{"set": num, "photos": photos})
		},
	}
}

func newLegoSetPhotoRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set-photo-remove <id>",
		Short:   "Remove one attached set photo — the wrong one attached by mistake, say",
		Example: "  wms lego set-photo-remove 4",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageError("%q isn't a photo ID", args[0])
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.RemoveSetPhoto(id); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Removed photo #%d", id)))
			return emit(map[string]any{"photo_id": id, "removed": true})
		},
	}
}
