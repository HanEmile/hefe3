./target/release/binsearch \
  '{ imul($size,_); _*; mov(rdi,$size); call(malloc,_); }' \
  ./target/release/binsearch

  # '{ pop(rsp); ret; }' \
  # { imul($size,_); _*; mov(rdi,$size); call(malloc,_); }
  # '{ call(free,$ptr); not:mov($ptr,_); not:lea($ptr,_); not:call(free,$ptr); call(_,$ptr); }' \
  # '{ call(free,$ptr); not:mov($ptr,_); not:call(free,$ptr); call(_,$ptr); }' \
  # '{ push($r); not:push(_); pop($r); }' \
  # '{ mov(rsp,$x); ret; }' \
  # '{ call(free,$ptr); not:mov($ptr,_); not:call(free,$ptr); call(_,$ptr); }' \
  #
  # { call(free,$ptr); not:mov($ptr,_); not:lea($ptr,_); not:call(free,$ptr); call(_,$ptr); }
