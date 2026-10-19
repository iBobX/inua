# inua-term's shell integration for fish, found through XDG_DATA_DIRS:
# the prompt marks (OSC 133) and the folder (OSC 7). Written by inua-term;
# changes are lost.
if status is-interactive; and not set -q __inua_term
    set -g __inua_term 1

    function __inua_prompt --on-event fish_prompt
        printf '\e]7;file://%s%s\a' (hostname) (string escape --style=url -- $PWD | string replace -a '%2F' /)
        printf '\e]133;A\a'
    end

    function __inua_preexec --on-event fish_preexec
        printf '\e]133;C;cmdline_url=%s\a' (string escape --style=url -- $argv[1])
    end

    function __inua_postexec --on-event fish_postexec
        printf '\e]133;D;%s\a' $status
    end

    # The input's start at the end of the prompt.
    if functions -q fish_prompt
        functions -c fish_prompt __inua_fish_prompt
        function fish_prompt
            __inua_fish_prompt
            printf '\e]133;B\a'
        end
    end
end
