The html output shouldn't contain the full file for each step in the taint (that's just too much data), but the surrounding lines are fine.

Please make it more structured, currently, the single line is just weird:
FROM /Users/emile/src/github.com/microsoft/WSL/src/linux/init/drvfs.cpp:119:27 (result) ← getenv(WSL_DRVFS_ELEVATED_ENV) ↓ TO /Users/emile/src/github.com/microsoft/WSL/src/linux/init/drvfs.cpp:119:27 (result) ← getenv(WSL_DRVFS_ELEVATED_ENV)

That's just to cluttered, structure this better and present it in an eye-catching way, I want the relevant details

Also, printing !!FULL TAINT in front of all findings is nonesense.

Try to group the findings according to their sinks and sources

Overall: make all the stuff search and filterable
