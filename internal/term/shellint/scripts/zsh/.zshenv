# inua-term's shell integration for zsh, found through ZDOTDIR: the
# user's own ZDOTDIR comes back first, so their files load as always;
# then the prompt marks (OSC 133) and the folder (OSC 7). Written by
# inua-term; changes are lost.
if [[ -n ${INUA_TERM_ZDOTDIR-} ]]; then
	ZDOTDIR=$INUA_TERM_ZDOTDIR
else
	unset ZDOTDIR
fi
unset INUA_TERM_ZDOTDIR
[[ -r ${ZDOTDIR:-$HOME}/.zshenv ]] && source ${ZDOTDIR:-$HOME}/.zshenv

if [[ -o interactive && -z ${__inua_term-} ]]; then
	typeset -g __inua_term=1 __inua_ran=0
	autoload -Uz add-zsh-hook

	__inua_precmd() {
		local st=$?
		if (( __inua_ran )); then
			printf '\e]133;D;%s\a' $st
			__inua_ran=0
		fi
		local LC_ALL=C s=$PWD out= c i
		for (( i = 1; i <= ${#s}; i++ )); do
			c=${s[i]}
			case $c in
			[a-zA-Z0-9./_~-]) out+=$c ;;
			*) out+=$(printf '%%%02X' "'$c") ;;
			esac
		done
		printf '\e]7;file://%s%s\a' "$HOST" "$out"
		printf '\e]133;A\a'
		# The input's mark goes on in a hook kept last, after a theme's
		# own has set PS1.
		precmd_functions=(${precmd_functions:#__inua_precmd_end} __inua_precmd_end)
		__inua_precmd_end # this time: zsh runs the hooks it had when the prompt began
	}

	__inua_precmd_end() {
		[[ $PS1 == *'133;B'* ]] || PS1+=$'%{\e]133;B\a%}'
	}

	__inua_preexec() {
		__inua_ran=1
		printf '\e]133;C\a'
	}

	add-zsh-hook precmd __inua_precmd
	add-zsh-hook preexec __inua_preexec
fi
