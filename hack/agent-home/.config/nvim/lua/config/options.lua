-- Options are automatically loaded before lazy.nvim startup
-- Default options that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/options.lua
-- Add any additional options here
local system_theme = require("config.system_theme")

vim.o.background = system_theme.get()
system_theme.setup()
vim.opt.clipboard = "unnamedplus"

-- The Windows clipboard is reachable through clip.exe, which WSL puts on PATH and the agent
-- container does not. Guarded rather than branched on the environment: an unguarded provider makes
-- every yank inside the container fail with a shell error.
if vim.fn.executable("clip.exe") == 1 then
  vim.g.clipboard = {
    name = "WslClipboard",
    copy = {
      ["+"] = "clip.exe",
      ["*"] = "clip.exe",
    },
    paste = {
      ["+"] = 'powershell.exe -NoLogo -NoProfile -c [Console]::Out.Write($(Get-Clipboard -Raw).ToString().Replace("`r", ""))',
      ["*"] = 'powershell.exe -NoLogo -NoProfile -c [Console]::Out.Write($(Get-Clipboard -Raw).ToString().Replace("`r", ""))',
    },
    cache_enabled = 0,
  }
end
vim.opt.wrap = true
vim.opt.linebreak = true
vim.opt.breakindent = true
