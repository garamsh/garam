# Prints `<process> <NAME>` for every environment variable a rendered
# object sets, from `helm template` output. The process is the source
# set the object's program reads, taken from the object's name: the name
# is `<prefix>-<word>`, and a word the chart gives a program of its own
# ends it (cowork-web before cowork), and a Job's name goes on with
# `-migrate-<run>`. Every other object runs the garam binary, whose three
# subcommands and migrate Job share one set of names, so `platform`
# says no more than that (README §What the checks are complete over).
/^apiVersion:/ { name = "" }
/^  name: / && name == "" {
	name = $2
	process = "platform"
	if (name ~ /-cowork-web$/) process = "coworkweb"
	else if (name ~ /-cowork(-migrate-.*)?$/) process = "cowork"
	else if (name ~ /-policy(-migrate-.*)?$/) process = "policy"
	else if (name ~ /-console$/) process = "console"
	else if (name ~ /-site$/) process = "site"
}
/^      - name: [A-Z][A-Z0-9_]*$/ { print process, $3 }
