#!/bin/sh

. "test/testlib.sh"

begin_test "uninit outside repository"
(
  set -e

  tmphome="$(basename "$0" ".sh")"
  mkdir -p $tmphome
  cp $HOME/.gitconfig $tmphome/
  HOME=$PWD/$tmphome
  cd $HOME

  [ "git-lfs smudge %f" = "$(git config filter.lfs.smudge)" ]
  [ "git-lfs clean %f" = "$(git config filter.lfs.clean)" ]

  git lfs uninit | tee uninit.log
  grep "configuration has been removed" uninit.log

  [ "" = "$(git config filter.lfs.smudge)" ]
  [ "" = "$(git config filter.lfs.clean)" ]
)
end_test

begin_test "uninit inside repository with default pre-push hook"
(
  set -e

  tmphome="$(basename "$0" ".sh")"
  mkdir -p $tmphome
  cp $HOME/.gitconfig $tmphome/
  HOME=$PWD/$tmphome
  cd $HOME

  reponame="$(basename "$0" ".sh")-hook"
  mkdir "$reponame"
  cd "$reponame"
  git init
  git lfs init

  [ -f .git/hooks/pre-push ]
  grep "git-lfs" .git/hooks/pre-push

  [ "git-lfs smudge %f" = "$(git config filter.lfs.smudge)" ]
  [ "git-lfs clean %f" = "$(git config filter.lfs.clean)" ]

  git lfs uninit

  [ -f .git/hooks/pre-push ] && {
    echo "expected .git/hooks/pre-push to be deleted"
    exit 1
  }
  [ "" = "$(git config filter.lfs.smudge)" ]
  [ "" = "$(git config filter.lfs.clean)" ]
)
end_test

begin_test "uninit inside repository without git lfs pre-push hook"
(
  set -e

  tmphome="$(basename "$0" ".sh")"
  mkdir -p $tmphome
  cp $HOME/.gitconfig $tmphome/
  HOME=$PWD/$tmphome
  cd $HOME

  reponame="$(basename "$0" ".sh")-no-hook"
  mkdir "$reponame"
  cd "$reponame"
  git init
  git lfs init
  echo "something something git-lfs" > .git/hooks/pre-push


  [ -f .git/hooks/pre-push ]
  [ "something something git-lfs" = "$(cat .git/hooks/pre-push)" ]

  [ "git-lfs smudge %f" = "$(git config filter.lfs.smudge)" ]
  [ "git-lfs clean %f" = "$(git config filter.lfs.clean)" ]

  git lfs uninit

  [ -f .git/hooks/pre-push ]
  [ "" = "$(git config filter.lfs.smudge)" ]
  [ "" = "$(git config filter.lfs.clean)" ]
)
end_test

begin_test "uninit hooks inside repository"
(
  set -e

  tmphome="$(basename "$0" ".sh")"
  mkdir -p $tmphome
  cp $HOME/.gitconfig $tmphome/
  HOME=$PWD/$tmphome
  cd $HOME

  reponame="$(basename "$0" ".sh")-only-hook"
  mkdir "$reponame"
  cd "$reponame"
  git init
  git lfs init

  [ -f .git/hooks/pre-push ]
  grep "git-lfs" .git/hooks/pre-push

  [ "git-lfs smudge %f" = "$(git config filter.lfs.smudge)" ]
  [ "git-lfs clean %f" = "$(git config filter.lfs.clean)" ]

  git lfs uninit hooks

  [ -f .git/hooks/pre-push ] && {
    echo "expected .git/hooks/pre-push to be deleted"
    exit 1
  }

  [ "git-lfs smudge %f" = "$(git config filter.lfs.smudge)" ]
  [ "git-lfs clean %f" = "$(git config filter.lfs.clean)" ]
)
end_test
