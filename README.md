Git Large File Storage
======

Git LFS is a command line extension for managing large files.  It replaces
large files with text pointers inside Git, while storing the actual files in a
remote Git LFS server.

The Git LFS client is written in Go, with pre-compiled binaries available for
Mac, Windows, Linux, and FreeBSD.

See [CONTRIBUTING.md](CONTRIBUTING.md) for info on working on Git LFS and
sending patches.

## Getting Started

Download the [latest client][rel] and run the included install script.  The
installer should run `git lfs init` for you, which sets up Git's global
configuration settings for Git LFS.

[rel]: https://github.com/github/git-lfs/releases

### Configuration

Git LFS uses `.gitattributes` files to configure which are managed by Git LFS.
Here is a sample one that saves zips and mp3s:

    $ cat .gitattributes
    *.mp3 filter=lfs -crlf
    *.zip filter=lfs -crlf

Git LFS can manage `.gitattributes` for you:

    $ git lfs track "*.mp3"
    Tracking *.mp3

    $ git lfs track "*.zip"
    Tracking *.zip

    $ git lfs track
    Listing tracked paths
        *.mp3 (.gitattributes)
        *.zip (.gitattributes)

    $ git lfs untrack "*.zip"
    Untracking *.zip

    $ git lfs track
    Listing tracked paths
        *.mp3 (.gitattributes)

### Pushing commits

Once setup, you're ready to push some commits.

    $ git add my.zip
    $ git commit -m "add zip"

You can confirm that Git LFS is managing your zip file:

    $ git lfs ls-files
    my.zip

Once you've made your commits, push your files to the Git remote.

    $ git push origin master
    Sending my.zip
    12.58 MB / 12.58 MB  100.00 %
    Counting objects: 2, done.
    Delta compression using up to 8 threads.
    Compressing objects: 100% (5/5), done.
    Writing objects: 100% (5/5), 548 bytes | 0 bytes/s, done.
    Total 5 (delta 1), reused 0 (delta 0)
    To https://github.com/github/git-lfs-test
       67fcf6a..47b2002  master -> master

See the [Git LFS overview](https://github.com/github/git-lfs/tree/master/docs) and [man pages](https://github.com/github/git-lfs/tree/master/docs/man).
