#!/bin/sh

. "test/testlib.sh"

begin_test "pre-push"
(
  set -e

  reponame="$(basename "$0" ".sh")"
  setup_remote_repo "$reponame"

  clone_repo "$reponame" repo
  git lfs track "*.dat"
  git add .gitattributes
  git commit -m "add git attributes"

  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  grep "(0 of 0 files) 0 B 0" push.log

  git lfs track "*.dat"
  echo "hi" > hi.dat
  git add hi.dat
  git commit -m "add hi.dat"
  git show

  # file isn't on the git lfs server yet
  curl -v "$GITSERVER/$reponame.git/info/lfs/objects/98ea6e4f216f2fb4b69fff9b3a44842c38686ca685f3f55dc48c5d3fb1107be4" \
    -u "user:pass" \
    -H "Accept: application/vnd.git-lfs+json" 2>&1 |
    tee http.log

  grep "404 Not Found" http.log

  # push file to the git lfs server
  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  grep "(1 of 1 files)" push.log

  # now the file exists
  curl -v "$GITSERVER/$reponame.git/info/lfs/objects/98ea6e4f216f2fb4b69fff9b3a44842c38686ca685f3f55dc48c5d3fb1107be4" \
    -u "user:pass" \
    -o lfs.json \
    -H "Accept: application/vnd.git-lfs+json" 2>&1 |
    tee http.log
  grep "200 OK" http.log

  grep "download" lfs.json || {
    cat lfs.json
    exit 1
  }
)
end_test

begin_test "pre-push dry-run"
(
  set -e

  reponame="$(basename "$0" ".sh")-dry-run"
  setup_remote_repo "$reponame"

  clone_repo "$reponame" repo-dry-run
  git lfs track "*.dat"
  git add .gitattributes
  git commit -m "add git attributes"

  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push --dry-run origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log

  [ "" = "$(cat push.log)" ]

  git lfs track "*.dat"
  echo "dry" > hi.dat
  git add hi.dat
  git commit -m "add hi.dat"
  git show

  # file doesn't exist yet
  curl -v "$GITSERVER/$reponame.git/info/lfs/objects/2840e0eafda1d0760771fe28b91247cf81c76aa888af28a850b5648a338dc15b" \
    -u "user:pass" \
    -H "Accept: application/vnd.git-lfs+json" 2>&1 |
    tee http.log
  grep "404 Not Found" http.log

  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push --dry-run origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  grep "push hi.dat" push.log

  # file still doesn't exist
  curl -v "$GITSERVER/$reponame.git/info/lfs/objects/2840e0eafda1d0760771fe28b91247cf81c76aa888af28a850b5648a338dc15b" \
    -u "user:pass" \
    -H "Accept: application/vnd.git-lfs+json" 2>&1 |
    tee http.log
  grep "404 Not Found" http.log
)
end_test

begin_test "pre-push with existing file"
(
  set -e

  reponame="$(basename "$0" ".sh")-existing-file"
  setup_remote_repo "$reponame"

  clone_repo "$reponame" existing-file
  echo "existing" > existing.dat
  git add existing.dat
  git commit -m "add existing dat"

  git lfs track "*.dat"
  echo "new" > new.dat
  git add new.dat
  git add .gitattributes
  git commit -m "add new file through git lfs"

  # push file to the git lfs server
  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  grep "(1 of 1 files)" push.log

  # now the file exists
  curl -v "$GITSERVER/$reponame.git/info/lfs/objects/7aa7a5359173d05b63cfd682e3c38487f3cb4f7f1d60659fe59fab1505977d4c" \
    -u "user:pass" \
    -o lfs.json \
    -H "Accept: application/vnd.git-lfs+json" 2>&1 |
    tee http.log
  grep "200 OK" http.log

  grep "download" lfs.json || {
    cat lfs.json
    exit 1
  }
)
end_test

begin_test "pre-push with existing pointer"
(
  set -e

  reponame="$(basename "$0" ".sh")-existing-pointer"
  setup_remote_repo "$reponame"
  clone_repo "$reponame" existing-pointer

  echo "$(pointer "7aa7a5359173d05b63cfd682e3c38487f3cb4f7f1d60659fe59fab1505977d4c" 4)" > new.dat
  git add new.dat
  git commit -m "add new pointer"
  mkdir -p .git/lfs/objects/7a/a7
  echo "new" > .git/lfs/objects/7a/a7/7aa7a5359173d05b63cfd682e3c38487f3cb4f7f1d60659fe59fab1505977d4c

  # push file to the git lfs server
  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  grep "(1 of 1 files)" push.log
)
end_test

begin_test "pre-push with missing pointer"
(
  set -e

  reponame="$(basename "$0" ".sh")-missing-pointer"
  setup_remote_repo "$reponame"
  clone_repo "$reponame" missing-pointer

  echo "$(pointer "7aa7a5359173d05b63cfd682e3c38487f3cb4f7f1d60659fe59fab1505977d4c" 4)" > new.dat
  git add new.dat
  git commit -m "add new pointer"

  # assert that push fails
  set +e
  echo "refs/heads/master master refs/heads/master 0000000000000000000000000000000000000000" |
    git lfs pre-push origin "$GITSERVER/$reponame" 2>&1 |
    tee push.log
  set -e
  grep "new.dat is an LFS pointer to 7aa7a5359173d05b63cfd682e3c38487f3cb4f7f1d60659fe59fab1505977d4c, which does not exist in .git/lfs/objects" push.log
)
end_test
