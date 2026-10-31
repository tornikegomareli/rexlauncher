-- Config code. rexd loads this file into Rex's config.

local plugin = rexd.plugin

rex.action{
  name = "jump_anywhere",
  title = "Jump Anywhere",
  run = function(ctx)
    return rex.session.new_layer{
      bounds = { 0.06, 0.06, 0.88, 0.88 },
      focus = true,
      layout = rex.layout.block{
        flavor = "com.superlogical.terminal.shell",
        options = { command = { plugin.dir .. "/bin/rexlauncher", "pick", ctx.client_id or "" } },
      },
    }
  end,
}

-- Keys in the user's init.lua win over this one.
rex.bind("ctrl+alt+shift+j", "jump_anywhere")
