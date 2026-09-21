-- Go: LSP-driven highlighting on top of LazyVim's lang.go extra.
--
-- Two highlighting layers are in play and they are not redundant:
--   * Treesitter colours the syntax it can see in the file alone.
--   * gopls semantic tokens colour what only a type checker knows -- which identifier is a type,
--     an interface, a package name, or an immutable value. The lang.go extra turns them on
--     (init_options.semanticTokens) and works around gopls not advertising the capability.
--
-- This file adds the parts the extra leaves out: making gopls findable, a few analyses, and
-- highlight links so the semantic tokens actually look different from plain Treesitter output.

-- `go install` drops binaries in $GOPATH/bin, which is not on PATH in this shell. Neovim needs it
-- on PATH to find a hand-installed gopls before Mason's copy.
local gobin = vim.fs.normalize(vim.env.GOBIN or ((vim.env.GOPATH or (vim.env.HOME .. "/go")) .. "/bin"))
if vim.fn.isdirectory(gobin) == 1 and not string.find(vim.env.PATH or "", gobin, 1, true) then
  vim.env.PATH = gobin .. ":" .. vim.env.PATH
end

-- Semantic tokens land in @lsp.type.* and @lsp.typemod.* groups. Most colourschemes leave several
-- of them unlinked, so gopls' extra knowledge would render as ordinary text without this.
local function link_go_semantic_tokens()
  local links = {
    -- What gopls knows and the parser does not.
    ["@lsp.type.namespace.go"] = "@module",
    ["@lsp.type.type.go"] = "@type",
    ["@lsp.type.interface.go"] = "@type",
    ["@lsp.type.struct.go"] = "@type",
    ["@lsp.type.typeParameter.go"] = "@type.definition",
    ["@lsp.type.parameter.go"] = "@variable.parameter",
    ["@lsp.type.property.go"] = "@property",
    ["@lsp.type.function.go"] = "@function",
    ["@lsp.type.method.go"] = "@function.method",
    -- Modifiers: a readonly variable is a constant, a defining occurrence is a declaration.
    ["@lsp.typemod.variable.readonly.go"] = "@constant",
    ["@lsp.typemod.variable.defaultLibrary.go"] = "@constant.builtin",
    ["@lsp.typemod.function.defaultLibrary.go"] = "@function.builtin",
    ["@lsp.typemod.type.defaultLibrary.go"] = "@type.builtin",
  }
  for group, target in pairs(links) do
    vim.api.nvim_set_hl(0, group, { link = target, default = true })
  end
end

vim.api.nvim_create_autocmd("ColorScheme", {
  group = vim.api.nvim_create_augroup("go_semantic_tokens", { clear = true }),
  callback = link_go_semantic_tokens,
})
link_go_semantic_tokens()

return {
  {
    "nvim-treesitter/nvim-treesitter",
    opts = { ensure_installed = { "go", "gomod", "gowork", "gosum", "gotmpl" } },
  },

  {
    "neovim/nvim-lspconfig",
    opts = {
      -- Inlay hints are the other half of "let the type checker tell you what this is".
      -- LazyVim keeps the toggle on <leader>uh.
      inlay_hints = { enabled = true },
      servers = {
        gopls = {
          settings = {
            gopls = {
              -- Analyses the extra does not enable. These are the ones that catch real bugs in
              -- controller-style code: shadowed errors and values that are written but never read.
              analyses = {
                shadow = true,
                fieldalignment = false,
                unusedvariable = true,
              },
              -- Report known vulnerabilities in dependencies as diagnostics.
              vulncheck = "Imports",
              -- Keep gopls out of build output and local cluster state.
              directoryFilters = {
                "-.git",
                "-.vscode",
                "-.idea",
                "-.vscode-test",
                "-node_modules",
                "-bin",
                "-.local",
              },
            },
          },
        },
      },
    },
  },
}
