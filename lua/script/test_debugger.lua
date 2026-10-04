-- Manual debugging scratch for the emmy_core native module (EmmyLua IDE
-- debugger): dials the IDE listener at localhost:9966. Never run this from
-- the automated test suite — execute it by hand during a debug session.
local dbg = require('emmy_core')
dbg.tcpConnect('localhost', 9966)
