package commands

import (
	"os"
	"os/exec"
	"sync"

	"github.com/github/git-lfs/git"
	"github.com/github/git-lfs/lfs"
	"github.com/github/git-lfs/vendor/_nuts/github.com/spf13/cobra"
)

var (
	checkoutCmd = &cobra.Command{
		Use:   "checkout",
		Short: "Checks out LFS files into the working copy",
		Run:   checkoutCommand,
	}
)

func checkoutCommand(cmd *cobra.Command, args []string) {

	// Parameters are filters
	// firstly convert any pathspecs to the root of the repo, in case this is being executed in a sub-folder
	var rootedpaths = make([]string, len(args))
	inchan := make(chan string, 1)
	outchan, err := lfs.ConvertCwdFilesRelativeToRepo(inchan)
	if err != nil {
		Panic(err, "Could not checkout")
	}
	for _, arg := range args {
		inchan <- arg
		rootedpaths = append(rootedpaths, <-outchan)
	}
	close(inchan)
	checkoutWithIncludeExclude(rootedpaths, nil)
}

func init() {
	RootCmd.AddCommand(checkoutCmd)
}

func checkoutWithIncludeExclude(include []string, exclude []string) {
	ref, err := git.CurrentRef()
	if err != nil {
		Panic(err, "Could not checkout")
	}

	pointers, err := lfs.ScanTree(ref)
	if err != nil {
		Panic(err, "Could not scan for Git LFS files")
	}

	var wait sync.WaitGroup
	wait.Add(1)

	c := make(chan *lfs.WrappedPointer)

	go func() {
		checkoutWithChan(c)
		wait.Done()
	}()
	for _, pointer := range pointers {
		if lfs.FilenamePassesIncludeExcludeFilter(pointer.Name, include, exclude) {
			c <- pointer
		}

	}
	close(c)
	wait.Wait()

}

func checkoutAll() {
	checkoutWithIncludeExclude(nil, nil)
}

// Populate the working copy with the real content of objects where the file is
// either missing, or contains a matching pointer placeholder, from a list of pointers.
// If the file exists but has other content it is left alone
func checkoutWithChan(in <-chan *lfs.WrappedPointer) {
	// Fire up the update-index command
	cmd := exec.Command("git", "update-index", "-q", "--refresh", "--stdin")
	updateIdxStdin, err := cmd.StdinPipe()
	if err != nil {
		Panic(err, "Could not update the index")
	}

	if err := cmd.Start(); err != nil {
		Panic(err, "Could not update the index")
	}

	// Get a converter from repo-relative to cwd-relative
	// Since writing data & calling git update-index must be relative to cwd
	repopathchan := make(chan string, 1)
	cwdpathchan, err := lfs.ConvertRepoFilesRelativeToCwd(repopathchan)
	if err != nil {
		Panic(err, "Could not convert file paths")
	}

	// As files come in, write them to the wd and update the index
	for pointer := range in {

		// Check the content - either missing or still this pointer (not exist is ok)
		filepointer, err := lfs.DecodePointerFromFile(pointer.Name)
		if err != nil && !os.IsNotExist(err) {
			if err == lfs.NotAPointerError {
				// File has non-pointer content, leave it alone
				continue
			}
			Panic(err, "Problem accessing %v", pointer.Name)
		}
		if filepointer != nil && filepointer.Oid != pointer.Oid {
			// User has probably manually reset a file to another commit
			// while leaving it a pointer; don't mess with this
			continue
		}
		// OK now we can (over)write the file content
		repopathchan <- pointer.Name
		cwdfilepath := <-cwdpathchan
		err = lfs.PointerSmudgeToFile(cwdfilepath, pointer.Pointer, nil)
		if err != nil {
			Panic(err, "Could not checkout file")
		}

		updateIdxStdin.Write([]byte(cwdfilepath + "\n"))
	}
	close(repopathchan)

	updateIdxStdin.Close()
	if err := cmd.Wait(); err != nil {
		Panic(err, "Error updating the git index")
	}

}
