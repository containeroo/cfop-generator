# bash completion for cfop-generator
_cfop_generator_completion() {
  local cur words cword
  _init_completion || return

  if [[ ${cword} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "completion version" -- "${cur}") )
    return
  fi

  if [[ ${words[1]} == "completion" && ${cword} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
    return
  fi

  COMPREPLY=( $(compgen -W "-file -proxied -output --version" -- "${cur}") )
}
complete -F _cfop_generator_completion cfop-generator
