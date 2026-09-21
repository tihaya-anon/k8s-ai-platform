local function fzf_files()
  require("fzf-lua").files({ cwd = LazyVim.root() })
end

return {
  {
    "folke/snacks.nvim",
    keys = {
      { "<leader><space>", false },
      { "<leader>ff", false },
      { "<leader>fg", false },
      { "<leader>fb", false },
    },
  },
  {
    "ibhagwan/fzf-lua",
    dependencies = {
      "nvim-tree/nvim-web-devicons",
    },
    opts = {
      "default-title",
      winopts = {
        height = 0.85,
        width = 0.82,
        preview = {
          layout = "flex",
          vertical = "down:45%",
          horizontal = "right:55%",
        },
      },
      fzf_opts = {
        ["--cycle"] = true,
      },
      defaults = {
        formatter = "path.filename_first",
      },
    },
    keys = {
      { "<leader><leader>", fzf_files, desc = "Find files (fzf)" },
      { "<leader>ff", fzf_files, desc = "Find files (fzf)" },
      { "<leader>fg", "<cmd>FzfLua live_grep<cr>", desc = "Live grep (fzf)" },
      { "<leader>fb", "<cmd>FzfLua buffers<cr>", desc = "Buffers (fzf)" },
    },
  },
}
