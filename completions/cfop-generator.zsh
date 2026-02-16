#compdef cfop-generator

local -a commands
commands=(
  'completion:Generate shell completion scripts'
  'version:Print the CLI version'
)

if (( CURRENT == 2 )); then
  _describe 'command' commands
  return
fi

case "${words[2]}" in
  completion)
    _values 'shell' bash zsh
    ;;
  *)
    _arguments '-file[Path to the exported zonefile]:file:_files' '--output[Output file written with mode 0600]:file:_files' '--proxied[Whether records should be proxied]' '--version[Print version]'
    ;;
esac
