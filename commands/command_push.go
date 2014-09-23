package commands

import (
	"fmt"
	"github.com/github/git-media/git"
	"github.com/github/git-media/gitmedia"
	"github.com/github/git-media/gitmediaclient"
	"github.com/github/git-media/pointer"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"strings"
)

var (
	pushCmd = &cobra.Command{
		Use:   "push",
		Short: "Push files to the media endpoint",
		Run:   pushCommand,
	}
	dryRun       = false
	deleteBranch = "(delete)"
)

func pushCommand(cmd *cobra.Command, args []string) {
	refsData, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		Panic(err, "Error reading refs on stdin")
	}

	if len(refsData) == 0 {
		return
	}

	refs := strings.Split(strings.TrimSpace(string(refsData)), " ")

	if refs[0] == deleteBranch {
		return
	}

	var left, right string

	if len(refs) > 1 {
		left = refs[1]
	}

	if len(refs) > 3 {
		right = "^" + refs[3]
	}

	revList, err := git.RevListObjects(left, right)
	if err != nil {
		Panic(err, "Error running git rev-list --objects %s %s", left, right)
	}

	links := make([]*pointer.Link, 0)
	for _, sha1 := range revList {
		link, err := pointer.FindLink(sha1)
		if err != nil {
			continue
		}

		links = append(links, link)
	}

	for i, link := range links {
		if dryRun {
			fmt.Println("push", link.Oid, link.Name)
			continue
		}
		if wErr := pushAsset(link.Oid, link.Name, i+1, len(links)); wErr != nil {
			Panic(wErr.Err, wErr.Error())
		}
	}
}

func pushAsset(oid, filename string, index, totalFiles int) *gitmedia.WrappedError {
	path, err := gitmedia.LocalMediaPath(oid)
	if err != nil {
		return gitmedia.Errorf(err, "Error uploading file %s (%s)", filename, oid)
	}

	status, err := gitmediaclient.Options(path)
	if err != nil {
		return gitmedia.Errorf(err, "Error uploading file %s (%s)", filename, oid)
	}

	if status == 200 {
		return nil
	}

	cb, file, cbErr := gitmedia.CopyCallbackFile("push", filename, index, totalFiles)
	if cbErr != nil {
		Error(cbErr.Error())
	}

	err = gitmediaclient.Put(path, filename, cb)
	if file != nil {
		file.Close()
	}

	return nil
}

func init() {
	pushCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Do everything except actually send the updates")
	RootCmd.AddCommand(pushCmd)
}
