-- Keymaps are automatically loaded on the VeryLazy event
-- Default keymaps that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/keymaps.lua
-- Add any additional keymaps here
vim.keymap.set("n", "<leader>ac", function()
  Snacks.terminal("test -r ~/.bash_env && . ~/.bash_env; pi", {
    cwd = LazyVim.root(),
    win = {
      position = "right",
      width = 0.4,
    },
  })
end, { desc = "Codex" })

vim.keymap.set("x", "<leader>b", 'c**<C-r>"**<Esc>', { desc = "Bold selection" })

vim.keymap.set("n", "K", "5k", { desc = "Move Up 5 Lines" })
vim.keymap.set("n", "J", "5j", { desc = "Move Down 5 Lines" })

vim.keymap.set("n", "<leader>cy", function()
  local diagnostics = vim.diagnostic.get(0, { lnum = vim.fn.line(".") - 1 })
  if diagnostics[1] then
    vim.fn.setreg("+", diagnostics[1].message)
    vim.notify("Copied diagnostic")
  else
    vim.notify("No diagnostic on current line", vim.log.levels.WARN)
  end
end, { desc = "Copy Diagnostic" })
