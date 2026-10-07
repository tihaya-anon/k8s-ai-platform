-- Keymaps are automatically loaded on the VeryLazy event
-- Default keymaps that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/keymaps.lua
-- Add any additional keymaps here
vim.keymap.set("n", "<leader>ac", function()
  Snacks.terminal("codex", {
    cwd = LazyVim.root(),
    win = {
      position = "right",
      width = 0.4,
    },
  })
end, { desc = "Codex" })

vim.keymap.set("x", "<leader>b", 'c**<C-r>"**<Esc>', { desc = "Bold selection" })
