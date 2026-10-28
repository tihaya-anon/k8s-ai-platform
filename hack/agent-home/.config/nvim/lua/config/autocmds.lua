-- Autocmds are automatically loaded on the VeryLazy event
-- Default autocmds that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/autocmds.lua
-- Add any additional autocmds here

local function open_markdown_in_typora()
  local file = vim.api.nvim_buf_get_name(0)

  if file == "" then
    vim.notify("Save this Markdown file before opening Typora", vim.log.levels.WARN)
    return
  end

  if vim.bo.modified then
    vim.cmd.write()
  end

  local powershell = vim.fn.executable("powershell.exe") == 1 and "powershell.exe" or nil
  local wslpath = vim.fn.executable("wslpath") == 1 and "wslpath" or nil

  if not powershell or not wslpath then
    vim.notify("Typora preview needs WSL with powershell.exe and wslpath available", vim.log.levels.WARN)
    return
  end

  local winpath = vim.fn.system({ wslpath, "-w", file }):gsub("%s+$", "")
  if vim.v.shell_error ~= 0 or winpath == "" then
    vim.notify("Could not convert file path for Windows", vim.log.levels.ERROR)
    return
  end

  local script = [[
param([string]$file)
$paths = @(
  "$env:LOCALAPPDATA\Programs\Typora\Typora.exe",
  "$env:ProgramFiles\Typora\Typora.exe",
  "${env:ProgramFiles(x86)}\Typora\Typora.exe"
) | Where-Object { $_ -and (Test-Path $_) }

if ($paths.Count -gt 0) {
  Start-Process -FilePath $paths[0] -ArgumentList @($file)
  exit 0
}

Start-Process -FilePath "typora" -ArgumentList @($file)
]]

  local job = vim.fn.jobstart({ powershell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script, winpath }, {
    detach = true,
  })

  if job <= 0 then
    vim.notify("Could not start Typora", vim.log.levels.ERROR)
  end
end

vim.api.nvim_create_autocmd("FileType", {
  pattern = "markdown",
  callback = function(event)
    vim.keymap.set("n", "<leader>cp", open_markdown_in_typora, {
      buffer = event.buf,
      desc = "Preview in Typora",
    })
  end,
})
