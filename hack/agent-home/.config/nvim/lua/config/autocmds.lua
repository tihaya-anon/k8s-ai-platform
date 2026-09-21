-- Autocmds are automatically loaded on the VeryLazy event
-- Default autocmds that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/autocmds.lua
--
-- Add any additional autocmds here
-- with `vim.api.nvim_create_autocmd`
--
-- Or remove existing autocmds by their group name (which is prefixed with `lazyvim_` for the defaults)
-- e.g. vim.api.nvim_del_augroup_by_name("lazyvim_wrap_spell")
-- 删除 LazyVim 默认的 Markdown/text 自动换行 + spell 检查
pcall(vim.api.nvim_del_augroup_by_name, "lazyvim_wrap_spell")

local group = vim.api.nvim_create_augroup("user_wrap_spell", { clear = true })

-- 其他文本文件保持 LazyVim 原来的行为
vim.api.nvim_create_autocmd("FileType", {
  group = group,
  pattern = { "text", "plaintex", "typst", "gitcommit" },
  callback = function()
    vim.opt_local.wrap = true
    vim.opt_local.spell = true
  end,
})

-- Markdown 保留自动换行，但关闭英文 spell 检查
vim.api.nvim_create_autocmd("FileType", {
  group = group,
  pattern = "markdown",
  callback = function()
    vim.opt_local.wrap = true
    vim.opt_local.linebreak = true
    vim.opt_local.breakindent = true
    vim.opt_local.spell = false
  end,
})

vim.api.nvim_create_autocmd("FocusGained", {
  callback = function()
    require("config.system_theme").update()
  end,
})
