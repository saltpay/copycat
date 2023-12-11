# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

## Copycat functions

Copycat currently provides the following functionality:
- `yaml-changer.sh` - a script that changes a value in a yaml file, and bumps the helm chart version if required
- `find-and-replace.sh` - a script that finds all instances of a string within a repo and replaces it with another string
## Example

Let's say you want to set the `pdb.maxUnavailable` value in your `values.yaml` file to `2`. Ideally, you would make this change in one repo, and then Copycat would copy the change to all other repos.

We're not quite there yet, so you'll have to use your imagination.

Currently we have an `yaml-changer.sh` script that does that change, and bumps the helm chart version. Now let's make that change in all repos.

Start copycat using `./start.sh`. Choose your target repos and watch Copycat start copying. You should be able to track progress in the logs, and see the pull requests being created in the target repos.
