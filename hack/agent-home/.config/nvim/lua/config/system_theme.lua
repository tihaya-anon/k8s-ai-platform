local M = {}

local function system(command)
  local result = vim.system(command, { text = true }):wait()

  if result.code ~= 0 then
    return nil
  end

  return result.stdout or ""
end

local function is_wsl()
  local files = { "/proc/sys/kernel/osrelease", "/proc/version" }

  for _, file in ipairs(files) do
    local ok, lines = pcall(vim.fn.readfile, file)
    local text = ok and table.concat(lines, "\n"):lower() or ""

    if text:find("microsoft", 1, true) or text:find("wsl", 1, true) then
      return true
    end
  end

  return false
end

local function macos_theme()
  local result = vim
    .system({
      "defaults",
      "read",
      "-g",
      "AppleInterfaceStyle",
    }, { text = true })
    :wait()

  return result.code == 0 and "dark" or "light"
end

local function windows_theme(command)
  local stdout = system({
    command,
    "query",
    [[HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize]],
    "/v",
    "AppsUseLightTheme",
  })

  if not stdout then
    return nil
  end

  if stdout:find("0x0", 1, true) then
    return "dark"
  end

  if stdout:find("0x1", 1, true) then
    return "light"
  end

  return nil
end

local function linux_theme()
  local color_scheme = system({
    "gsettings",
    "get",
    "org.gnome.desktop.interface",
    "color-scheme",
  })

  if color_scheme and color_scheme:find("dark", 1, true) then
    return "dark"
  end

  local gtk_theme = system({
    "gsettings",
    "get",
    "org.gnome.desktop.interface",
    "gtk-theme",
  })

  if gtk_theme and gtk_theme:lower():find("dark", 1, true) then
    return "dark"
  end

  if color_scheme or gtk_theme then
    return "light"
  end

  return nil
end

local function reload_colorscheme()
  local colorscheme = vim.g.colors_name

  if colorscheme and colorscheme ~= "" then
    vim.schedule(function()
      pcall(vim.cmd.colorscheme, colorscheme)
    end)
  end
end

function M.get()
  local os = vim.uv.os_uname().sysname

  if os == "Darwin" then
    return macos_theme()
  end

  if os == "Linux" and is_wsl() then
    return windows_theme("reg.exe") or linux_theme() or "dark"
  end

  if os == "Linux" then
    return linux_theme() or "dark"
  end

  if os == "Windows_NT" then
    return windows_theme("reg") or "dark"
  end

  return "dark"
end

function M.update()
  local background = M.get()

  if vim.o.background == background then
    return
  end

  vim.o.background = background
  reload_colorscheme()
end

function M.setup()
  local group = vim.api.nvim_create_augroup("SystemTheme", { clear = true })

  vim.api.nvim_create_autocmd({ "FocusGained", "VimResume" }, {
    group = group,
    callback = M.update,
  })

  vim.api.nvim_create_user_command("SystemThemeUpdate", M.update, {})
end

return M
