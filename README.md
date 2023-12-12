# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

<img src="./copycat-logo.png"
     alt="Markdown Monster icon"
     style="margin-bottom: 10px;" />

## Copycat functions

Copycat currently provides the following functionality:
- `yaml-changer.sh` - a script that changes a value in a yaml file, and bumps the helm chart version if required
- `find-and-replace.sh` - a script that finds all instances of a string within a repo and replaces it with another string

# Some notes

- Copycat is currently only tested on Mac OS
- Copycat assumes that it is in the same directory as the repos you want to copy changes to
- Please make sure you have no unstaged changes in your repos before running Copycat

## Usage

Start Copycat by running `sh start.sh` from the root of this repository. Follow the instructions to make changes across your repos.

Copycat will walk you through every step, but for reference, here are the usages of the scripts:

### `yaml-changer.sh`

This script will change a value in a yaml file, and bump the helm chart version if required. For example:
`sh yaml-changer.sh -f ./path/to/file.yaml -k key -v value` will change the value of `key` to `value` in `file.yaml`.

### `find-and-replace.sh`

This script will find all instances of a string within a repo and replace it with another string. For example:
`sh find-and-replace.sh -f "find" -r "replace"` will find all instances of `"find"` in the repo and replace them with `"replace"`.
