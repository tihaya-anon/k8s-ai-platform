return {
  {
    "neovim/nvim-lspconfig",
    opts = {
      servers = {
        ["*"] = {
          keys = {
            { "K", false },
            { "<leader>ch", vim.lsp.buf.hover, desc = "Hover" },
          },
        },
      },
    },
  },
}
