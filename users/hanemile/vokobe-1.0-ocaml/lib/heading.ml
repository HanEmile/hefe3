type heading = {
  level : int;
  title : string;
  slug : string;
  number_str : string;
}

let update_counters (counters : int list) (level : int) : int list =
  let rec aux curr_level acc = function
    | [] ->
        if curr_level <= level then
          aux (curr_level + 1) (1 :: acc) []
        else List.rev acc
    | hd :: tl ->
        if curr_level = level then
          List.rev ((hd + 1) :: acc)
        else if curr_level < level then
          aux (curr_level + 1) (hd :: acc) tl
        else
          List.rev acc
  in
  aux 1 [] counters

let format_counter (counters : int list) : string =
  String.concat "." (List.map string_of_int counters) ^ "."

let slugify (s : string) : string =
  let buf = Buffer.create (String.length s) in
  let prev_is_dash = ref true in
  String.iter
    (fun c ->
      match c with
      | 'a' .. 'z' | '0' .. '9' ->
          Buffer.add_char buf c;
          prev_is_dash := false
      | 'A' .. 'Z' ->
          Buffer.add_char buf (Char.lowercase_ascii c);
          prev_is_dash := false
      | _ ->
          if not !prev_is_dash then (
            Buffer.add_char buf '-';
            prev_is_dash := true
          ))
    s;
  let res = Buffer.contents buf in
  let len = String.length res in
  if len > 0 && res.[len - 1] = '-' then
    String.sub res 0 (len - 1)
  else
    res

let parse_heading (line : string) : (int * string) option =
  let line = String.trim line in
  let len = String.length line in
  let rec count i =
    if i < len && line.[i] = '#' then count (i + 1) else i
  in
  let level = count 0 in
  if level = 0 || level > 5 || level >= len || line.[level] <> ' ' then
    None
  else
    Some (level, String.trim (String.sub line (level + 1) (len - level - 1)))

let collect_headings (raw : string) : heading list =
  let counters = Array.make 5 0 in
  raw
  |> String.split_on_char '\n'
  |> List.filter_map (fun line ->
       match parse_heading line with
       | None -> None
       | Some (level, title) ->
           counters.(level - 1) <- counters.(level - 1) + 1;
           for i = level to 4 do counters.(i) <- 0 done;
           let number =
             List.init level (fun i -> string_of_int counters.(i))
             |> String.concat "."
             |> fun s -> s ^ "."
           in
           Some {
             level;
             title;
             slug = slugify title;
             number_str = number ^ " " ^ title;
           })
