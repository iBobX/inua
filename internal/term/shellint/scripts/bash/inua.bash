# inua-term's shell integration for bash, read as the --rcfile: the user's
# ~/.bashrc first, then the prompt marks (OSC 133) and the folder (OSC 7)
# around the prompt. Written by inua-term; changes are lost.
[ -r ~/.bashrc ] && . ~/.bashrc

if [[ $- == *i* && -z ${__inua_term-} ]]; then
	__inua_term=1

	# The folder, as a file:// URL.
	__inua_osc7() {
		local LC_ALL=C s=$PWD out= c i
		for ((i = 0; i < ${#s}; i++)); do
			c=${s:i:1}
			case $c in
			[a-zA-Z0-9./_~-]) out+=$c ;;
			*) printf -v c '%%%02X' "'$c"; out+=$c ;;
			esac
		done
		printf '\e]7;file://%s%s\a' "${HOSTNAME-}" "$out"
	}

	# Before each prompt: the last command's end with its status (when one
	# ran), the folder, the prompt's start.
	__inua_prompt() {
		local status=$?
		if [[ -n ${__inua_ran-} ]]; then
			printf '\e]133;D;%s\a' "$status"
			__inua_ran=
		fi
		__inua_osc7
		printf '\e]133;A\a'
		return $status
	}

	# After the user's own prompt command (which may set PS1): the input's
	# start at the end of the prompt.
	__inua_prompt_end() {
		local status=$?
		[[ $PS1 == *'133;B'* ]] || PS1+='\[\e]133;B\a\]'
		return $status
	}

	if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
		PROMPT_COMMAND=(__inua_prompt "${PROMPT_COMMAND[@]}" __inua_prompt_end)
	else
		PROMPT_COMMAND="__inua_prompt${PROMPT_COMMAND:+; $PROMPT_COMMAND}; __inua_prompt_end"
	fi
	# A command starts: PS0 is drawn once it is read. The assignment inside
	# the mark notes that one ran, in this shell (not a subshell).
	PS0+='\e]133;C;inua=$((__inua_ran=1))\a'
fi
