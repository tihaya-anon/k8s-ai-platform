-- Options are automatically loaded before lazy.nvim startup
-- Default options that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/options.lua
-- Add any additional options here
local system_theme = require("config.system_theme")

vim.o.background = system_theme.get()
system_theme.setup()
vim.opt.clipboard = "unnamedplus"

-- The Windows clipboard is only reachable when the Windows binaries are on PATH, which is true in
-- WSL and false inside the agent container. Guarding rather than branching on the environment: an
-- unguarded `clip.exe` provider makes every yank in the container fail with a shell error.
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

-- Soft-wrap long lines instead of scrolling sideways. LazyVim already sets `linebreak` (wrap at
-- word boundaries, never mid-word) and already maps j/k to gj/gk, so movement follows the visual
-- line without further work. `breakindent` is the piece that matters for code: a wrapped line keeps
-- its indentation instead of jumping back to column one, so nesting stays readable.
vim.opt.wrap = true
vim.opt.breakindent = true
-- No continuation glyph: a wrapped line is marked purely by an extra two columns of indent, which
-- reads as continuation without stealing width or adding a character that gets copied out.
vim.opt.showbreak = ""
vim.opt.breakindentopt = "shift:2"
