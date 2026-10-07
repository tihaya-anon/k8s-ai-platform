return {
  {
    "folke/snacks.nvim",
    opts = {
      -- Centred floating terminal instead of a split: 80% of the editor width, 60% of its height.
      terminal = {
        win = {
          position = "float",
          width = 0.8,
          height = 0.6,
          border = "rounded",
        },
      },
    },
  },
}
