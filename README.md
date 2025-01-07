# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

<img src="./copycat-logo.png"
     alt="Copycat logo"
     style="margin-bottom: 10px; animation: spin 2s linear infinite; transform-origin: center center;" />

## Copycat functions

Copycat is an abstraction on top of [openrewrite](https://github.com/openrewrite/rewrite) that allows you to apply changes to multiple projects at once.

You can find community recipes [here](https://docs.openrewrite.org/recipes) and copy them to the `rewrite.yaml` file in the root of the project. 

# Some notes

- Copycat assumes that it is in the same directory as the repos you want to copy changes to
- Please make sure you have no unstaged changes in your repos before running Copycat

## Usage

Just type `go run main.go` and follow the prompts.
