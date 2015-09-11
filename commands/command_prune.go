package commands

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	"github.com/github/git-lfs/lfs"
	"github.com/github/git-lfs/vendor/_nuts/github.com/spf13/cobra"
)

var (
	pruneCmd = &cobra.Command{
		Use:   "prune",
		Short: "Deletes old LFS files from the local store",
		Run:   pruneCommand,
	}
	pruneDryRunArg      bool
	pruneVerboseArg     bool
	pruneVerifyArg      bool
	pruneDoNotVerifyArg bool
)

func pruneCommand(cmd *cobra.Command, args []string) {

	// Guts of this must be re-usable from fetch --prune so just parse & dispatch
	if pruneVerifyArg && pruneDoNotVerifyArg {
		Exit("Cannot specify both --verify-remote and --no-verify-remote")
	}

	verify := !pruneDoNotVerifyArg &&
		(lfs.Config.FetchPruneConfig().PruneVerifyRemoteAlways || pruneVerifyArg)

	prune(verify, pruneDryRunArg, pruneVerboseArg)

}

type PruneProgressType int

const (
	PruneProgressTypeLocal  = PruneProgressType(iota)
	PruneProgressTypeRetain = PruneProgressType(iota)
	PruneProgressTypeVerify = PruneProgressType(iota)
)

// Progress from a sub-task of prune
type PruneProgress struct {
	Type  PruneProgressType
	Count int // Number of items done
}
type PruneProgressChan chan PruneProgress

func prune(verifyRemote, dryRun, verbose bool) {
	localObjects := make([]*lfs.Pointer, 0, 100)
	retainedObjects := lfs.NewStringSetWithCapacity(100)
	var reachableObjects lfs.StringSet
	var taskwait sync.WaitGroup

	// Add all the base funcs to the waitgroup before starting them, in case
	// one completes really fast & hits 0 unexpectedly
	// each main process can Add() to the wg itself if it subdivides the task
	taskwait.Add(5) // 1..5: localObjects, current checkout, recent refs, unpushed, worktree
	if verifyRemote {
		taskwait.Add(1) // 6
	}

	progressChan := make(PruneProgressChan, 100)

	// Populate the single list of local objects
	go pruneTaskGetLocalObjects(&localObjects, progressChan, &taskwait)

	// Now find files to be retained from many sources
	retainChan := make(chan string, 100)

	go pruneTaskGetRetainedCurrentCheckout(retainChan, &taskwait)
	go pruneTaskGetRetainedRecentRefs(retainChan, &taskwait)
	go pruneTaskGetRetainedUnpushed(retainChan, &taskwait)
	go pruneTaskGetRetainedWorktree(retainChan, &taskwait)
	if verifyRemote {
		reachableObjects = lfs.NewStringSetWithCapacity(100)
		go pruneTaskGetReachableObjects(&reachableObjects, &taskwait)
	}

	// Now collect all the retained objects, on separate wait
	var retainwait sync.WaitGroup
	retainwait.Add(1)
	go pruneTaskCollectRetained(&retainedObjects, retainChan, progressChan, &retainwait)

	// Report progress
	var progresswait sync.WaitGroup
	progresswait.Add(1)
	go pruneTaskDisplayProgress(progressChan, &progresswait)

	taskwait.Wait()   // wait for subtasks
	close(retainChan) // triggers retain collector to end now all tasks have
	retainwait.Wait() // make sure all retained objects added

	prunableObjects := make([]string, 0, len(localObjects)/2)

	// Build list of prunables (also queue for verify at same time if applicable)
	var verifyQueue *lfs.TransferQueue
	var verifiedObjects lfs.StringSet
	var totalSize int64
	var verboseOutput bytes.Buffer
	if verifyRemote && !dryRun {
		lfs.Config.CurrentRemote = lfs.Config.FetchPruneConfig().PruneRemoteName
		// build queue now, no estimates or progress output
		verifyQueue = lfs.NewDownloadCheckQueue(0, 0, true)
		verifiedObjects = lfs.NewStringSetWithCapacity(len(localObjects) / 2)
	}
	for _, pointer := range localObjects {
		if !retainedObjects.Contains(pointer.Oid) {
			prunableObjects = append(prunableObjects, pointer.Oid)
			totalSize += pointer.Size
			if verbose {
				// Save up verbose output for the end, spinner still going
				verboseOutput.WriteString(fmt.Sprintf("Prune %v ,%v", pointer.Oid, humanizeBytes(pointer.Size)))
			}
			if verifyRemote && !dryRun {
				verifyQueue.Add(lfs.NewDownloadCheckable(&lfs.WrappedPointer{Pointer: pointer}))
			}
		}
	}
	if verifyRemote && !dryRun {
		// this channel is filled with oids for which Check() succeeded & Transfer() was called
		verifyc := verifyQueue.Watch()
		var verifywait sync.WaitGroup
		verifywait.Add(1)
		go func() {
			for oid := range verifyc {
				verifiedObjects.Add(oid)
				progressChan <- PruneProgress{PruneProgressTypeVerify, 1}
			}
			verifywait.Done()
		}()
		verifyQueue.Wait()
		verifywait.Wait()
		close(progressChan) // after verify (uses spinner) but before check
		progresswait.Wait()
		pruneCheckVerified(prunableObjects, reachableObjects, verifiedObjects)
	} else {
		close(progressChan)
		progresswait.Wait()
	}

	if dryRun {
		Print("%d files would be pruned, %v", len(prunableObjects), humanizeBytes(totalSize))
	} else {
		Print("Pruning %d files, %v", len(prunableObjects), humanizeBytes(totalSize))
		pruneDeleteFiles(prunableObjects)
	}

}

func pruneCheckVerified(prunableObjects []string, reachableObjects, verifiedObjects lfs.StringSet) {
	// There's no issue if an object is not reachable and misisng, only if reachable & missing
	var problems bytes.Buffer
	for _, oid := range prunableObjects {
		// Test verified first as most likely reachable
		if !verifiedObjects.Contains(oid) {
			if reachableObjects.Contains(oid) {
				problems.WriteString(fmt.Sprintf("%v\n", oid))
			}
		}
	}
	// technically we could still prune the other oids, but this indicates a
	// more serious issue because the local state implies that these can be
	// deleted but that's incorrect; bad state has occurred somehow, might need
	// push --all to resolve
	if problems.Len() > 0 {
		Exit("Failed to find prunable objects on remote, aborting:\n%v", problems.String())
	}
}

func pruneTaskDisplayProgress(progressChan PruneProgressChan, waitg *sync.WaitGroup) {
	defer waitg.Done()

	spinner := lfs.NewSpinner()
	localCount := 0
	retainCount := 0
	verifyCount := 0
	var msg string
	for p := range progressChan {
		switch p.Type {
		case PruneProgressTypeLocal:
			localCount++
		case PruneProgressTypeRetain:
			retainCount++
		case PruneProgressTypeVerify:
			verifyCount++
		}
		msg = fmt.Sprintf("%d local objects, %d retained", localCount, retainCount)
		if verifyCount > 0 {
			msg += fmt.Sprintf(", %d verified with remote", verifyCount)
		}
		spinner.Print(OutputWriter, msg)
	}
	spinner.Finish(OutputWriter, msg)
}

func pruneTaskCollectRetained(outRetainedObjects *lfs.StringSet, retainChan chan string,
	progressChan PruneProgressChan, retainwait *sync.WaitGroup) {

	defer retainwait.Done()

	for oid := range retainChan {
		outRetainedObjects.Add(oid)
		progressChan <- PruneProgress{PruneProgressTypeRetain, 1}
	}

}

func pruneDeleteFiles(prunableObjects []string) {
	spinner := lfs.NewSpinner()
	var problems bytes.Buffer
	// In case we fail to delete some
	var deletedFiles int
	for i, oid := range prunableObjects {
		spinner.Print(OutputWriter, fmt.Sprintf("Deleting object %d/%d", i, len(prunableObjects)))
		mediaFile, err := lfs.LocalMediaPath(oid)
		if err != nil {
			problems.WriteString(fmt.Sprintf("Unable to find media path for %v: %v\n", oid, err))
			continue
		}
		err = os.Remove(mediaFile)
		if err != nil {
			problems.WriteString(fmt.Sprintf("Failed to remove file %v: %v\n", mediaFile, err))
			continue
		}
		deletedFiles++
	}
	spinner.Finish(OutputWriter, fmt.Sprintf("Deleted %d files", deletedFiles))
	if problems.Len() > 0 {
		LoggedError(fmt.Errorf("Failed to delete some files"), problems.String())
		Exit("Prune failed, see errors above")
	}
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetLocalObjects(outLocalObjects *[]*lfs.Pointer, progChan PruneProgressChan, waitg *sync.WaitGroup) {
	defer waitg.Done()

	localObjectsChan := lfs.AllLocalObjectsChan()
	for p := range localObjectsChan {
		*outLocalObjects = append(*outLocalObjects, p)
		progChan <- PruneProgress{PruneProgressTypeLocal, 1}
	}
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetRetainedCurrentCheckout(retainChan chan string, waitg *sync.WaitGroup) {
	defer waitg.Done()

	// TODO
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetRetainedRecentRefs(retainChan chan string, waitg *sync.WaitGroup) {
	defer waitg.Done()

	// TODO
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetRetainedUnpushed(retainChan chan string, waitg *sync.WaitGroup) {
	defer waitg.Done()

	// TODO
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetRetainedWorktree(retainChan chan string, waitg *sync.WaitGroup) {
	defer waitg.Done()

	// TODO
}

// Background task, must call waitg.Done() once at end
func pruneTaskGetReachableObjects(outObjectSet *lfs.StringSet, waitg *sync.WaitGroup) {
	defer waitg.Done()

	// TODO
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneDryRunArg, "dry-run", "d", false, "Don't delete anything, just report")
	pruneCmd.Flags().BoolVarP(&pruneDryRunArg, "verbose", "v", false, "Print full details of what is/would be deleted")
	pruneCmd.Flags().BoolVarP(&pruneDryRunArg, "verify-remote", "c", false, "Verify that remote has LFS files before deleting")
	pruneCmd.Flags().BoolVar(&pruneDryRunArg, "no-verify-remote", false, "Override lfs.pruneverifyremotealways and don't verify")
	RootCmd.AddCommand(pruneCmd)
}
