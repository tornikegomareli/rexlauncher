-- Config code. rexd loads this file into Rex's config.

local plugin = rexd.plugin

-- The picker open in each session, so the key closes it.
local open_layers = {}

-- A picker closes itself too, and Rex reports no error for closing what is gone, so
-- the way to know is to look.
local function is_open(session_id, layer_id)
  local view = rex.session.view{ session_id = session_id }
  for _, window in ipairs(view and view.windows or {}) do
    for _, layer in ipairs(window.layers) do
      if layer.layer_id == layer_id then
        return true
      end
    end
  end
  return false
end

rex.action{
  name = "rexlauncher",
  title = "Rex Launcher",
  run = function(ctx)
    local session = ctx.session_id or ""
    local open = open_layers[session]
    open_layers[session] = nil
    if open and is_open(ctx.session_id, open) then
      rex.session.close_layer{ session_id = ctx.session_id, layer_id = open }
      return
    end
    local created = rex.session.new_layer{
      bounds = { 0.06, 0.06, 0.88, 0.88 },
      focus = true,
      layout = rex.layout.block{
        flavor = "com.superlogical.terminal.shell",
        options = { command = { plugin.dir .. "/bin/rexlauncher", "pick", ctx.client_id or "" } },
      },
    }
    open_layers[session] = created.layer_id
    return created
  end,
}

-- Keys in the user's init.lua win over this one.
rex.bind("ctrl+alt+shift+l", "rexlauncher")
