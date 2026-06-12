{ ... }:

let
  # merges the list of keys below with a list containing a key "all" with
  # the value being a list of all key values
  # src: the tvl monorepo, //users/tazjin/keys/default.nix as of 2025-10-07
  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll {
  # fetched from github on `2025-11-12 20:00 UTC+2`
  a = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDIK16slD9SEJkvgpaUyA+tCWATKo6xwwwp39T73pqb6Udp0MBr+aJQ3yUUVly1cRht36zctCB2pixkRkD2hx98LRbFASMrxNPbKf7g7uOT6he5YLhkS+z3tYqcrgbtZggnJwnPygPkDgtkfE04/caUG5Yz1bRFSkE9IYS5xYo9GIZaxqD+q8TekJPHXc5dX1ucEHtG7LpRz/C3HcjIx40xzz4vYtxNTOGpUNJAS0RbY4wlvMTspiCj5fld2VZ6F8yFygIm0Ty12UcjVHPQHZ+Oa1LvBkF36NZl1sYzwwt9lXSWbDvN/EeOefHbcQ69psX9xIewZDSOCcBApJEDRTIQDJhqkAaoDk0Sndk1vTRTRCLL+gSyHQHoL5WquS/MrYrSqPwhtd3q9homE8Oi2K2+KrRyY7zVmNNXVN0nOxYPHNMm5uwRdkDKd8pUH4SP7Wv75/eYrEGz4YnpYIitrElHhbScofJlOY4TqxiPFkypvmrsfrAw4HqGMU9NiG40mt5yqwufIqwjd64+JFJ/t/XFSJpEahSArncXnG9x+vlEk4O4APXvNzmRTM2ZlBP8lH1lVAxkFYMo10UQLaN8/jTqWYa5FDmLZPyKO9Bn4XYt7lTsOvFDC78NvUZUlBqNhvU5CFZOH/CNCffeav3Iux80lBTTqdKn0xq8wkI6MZXo6Q==";
}
