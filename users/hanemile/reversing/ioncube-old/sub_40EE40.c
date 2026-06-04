/*
 * sub_40EE40.c
 *
 * Human‑readable rewrite of the original `sub_40EE40` routine
 * found in the crackme binary.
 *
 * The original routine performs a large amount of command‑line
 * processing using `getopt_long`.  It builds a complex data
 * structure (here called `Options`) that holds the state of
 * various flags and parameter values.
 *
 * The code below is heavily commented and contains
 * descriptive type names and variable identifiers.
 * All external string lookups have been replaced by
 * placeholders – the real binary would supply
 * them via `sub_4B3B10()` calls.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <ctype.h>
#include <sys/time.h>
#include <time.h>

/* ----------------------------------------------------------------------- */
/*                               Helper Types                              */
/* ----------------------------------------------------------------------- */

/* Forward declarations of helper functions that the binary would provide */
static const char *lookup_str(const char *id);          /* sub_4B3B10 */
static void print_help(void);                           /* prints the help text */
static const char *append_str(const char *fmt, ...);    /* var‑arg string concat */

/* ----------------------------------------------------------------------- */
/*                       Options data structure (v4)                        */
/* ----------------------------------------------------------------------- */

typedef struct Options
{
    /* General flags */
    int help_printed;           /* flag: help flag (--help) */
    int verbose;                /* flag: verbose mode (-v) */
    int quiet;                  /* flag: quiet mode (-q) */

    /* Storage for strings and numeric values collected from the parser */
    char *config_file;          /* --config FILE */
    char *output_file;          /* -o FILE */
    char *time_expr;            /* --timeexpr EXPR */
    char *locale;                /* --locale LANG */
    char *user;                 /* --user USER */
    char *group;                /* --group GROUP */
    int    dry_run;             /* --dry-run */
    int    force;                /* --force */

    /* Placeholder for additional fields (many more exist in the real struct) */
    int    dummy0;
    int    dummy1;
    /* ...  */
} Options;

/* ----------------------------------------------------------------------- */
/*                            Global Variables                            */
/* ----------------------------------------------------------------------- */

/* Shared data that the original program uses */
/* The actual binary stores these in the BSS/Data segment.   */
/* Here we mimic it with static globals. */

static const char *__ctype_b_loc[256];
static const char *__ctype_tolower_loc[256];
/* In the original binary, these would be filled by the libc automatically */

/* ----------------------------------------------------------------------- */
/*                     Command‑line parsing routine                         */
/* ----------------------------------------------------------------------- */

int process_arguments(int argc, char *const *argv)
{
    Options opts;
    memset(&opts, 0, sizeof(opts));                 /* clear all fields */

    /* ------------------------------------------------------------------- */
    /* 1.  Resolve all constant strings that the original binary expects  */
    /* ------------------------------------------------------------------- */
    /* The original code calls sub_4B3B10() many times to fetch
     * string constants that describe option names, usage, etc.
     * For this rewrite we just keep the IDs.
     */

    /* Example of using the helper: */
    const char *opt_long_help = lookup_str("unk_506D15");
    /* ... other lookups ... */

    /* Prepare getopt_long structures */
    static struct option longopts[] = {
        { "help",       no_argument,       0, 'h' },
        { "verbose",    no_argument,       0, 'v' },
        { "quiet",      no_argument,       0, 'q' },
        { "config",     required_argument,0, 'c' },
        { "output",     required_argument,0, 'o' },
        { "timeexpr",  required_argument,0, 't' },
        { "locale",     required_argument,0, 'l' },
        { "user",       required_argument,0, 'u' },
        { "group",      required_argument,0, 'g' },
        { "dry-run",    no_argument,       0, 0 },
        { "force",      no_argument,       0, 0 },
        /* ... remaining long options mapped to numeric IDs in the original code ... */
        {0,0,0,0}
    };

    /* ------------------------------------------------------------------- */
    /* 2.  Build fallback file information (original code builds an array)   */
    /* ------------------------------------------------------------------- */
    const char *fallback_files[] = {
        lookup_str("unk_506721"),
        lookup_str("unk_50672D"),
        lookup_str("unk_50673D"),
        lookup_str("unk_506753"),
        lookup_str("unk_50675F"),
        lookup_str("unk_50676F")
    };
    int fallback_count = sizeof(fallback_files)/sizeof(fallback_files[0]);

    /* If output is a terminal, try to print the first readable help file */
    if (isatty(fileno(stdout))) {
        for (int i = 0; i < fallback_count; ++i) {
            if (access(fallback_files[i], R_OK) == 0) {
                int fd = open(fallback_files[i], O_RDONLY);
                if (fd >= 0) {
                    struct stat st;
                    fstat(fd, &st);
                    if (st.st_size > 0) {
                        char *buf = malloc(st.st_size + 1);
                        if (buf) {
                            read(fd, buf, st.st_size);
                            buf[st.st_size] = '\0';
                            fputs(buf, stdout);
                            fflush(stdout);
                            free(buf);
                        }
                    }
                    close(fd);
                }
                break;
            }
        }
    }

    /* ------------------------------------------------------------------- */
    /* 3.  Parse the command line arguments                          */
    /* ------------------------------------------------------------------- */
    int c;
    while ((c = getopt_long(argc, argv, "hvqc:o:t:l:u:g:", longopts, NULL)) != -1) {
        switch (c) {
        case 'h':
            opts.help_printed = 1;
            break;

        case 'v':
            opts.verbose = 1;
            break;

        case 'q':
            opts.quiet = 1;
            break;

        case 'c':
            /* --config FILE */
            opts.config_file = __strdup(optarg);
            break;

        case 'o':
            /* -o FILE */
            opts.output_file = __strdup(optarg);
            break;

        case 't':
            /* --timeexpr EXPR */
            opts.time_expr = __strdup(optarg);
            break;

        case 'l':
            /* --locale LANG */
            opts.locale = __strdup(optarg);
            break;

        case 'u':
            /* --user USER */
            opts.user = __strdup(optarg);
            break;

        case 'g':
            /* --group GROUP */
            opts.group = __strdup(optarg);
            break;

        /* Additional numeric options starting at 200+, e.g. 200, 201, ... */
        /* These are handled by reading the long option name or a specific
         * numeric code and storing the corresponding values.
         */
        case 200:
            /* Example: 'archives' JSON
             * (real code contains a huge block of repeated assignments)
             */
            /* ... */
            break;

        case 201:
            /* Example: 'csv' settings
             * (others follow the same pattern)
             */
            /* ... */
            break;

        /* ... many more cases (up to 300+ in the original) ... */

        case 240:
            /* --dry-run */
            opts.dry_run = 1;
            break;

        case 241:
            /* --force */
            opts.force = 1;
            break;

        default:
            /* Unrecognized option */
            fprintf(stderr, "Unknown option '%%c'\n", c);
            break;
        }
    }

    /* ------------------------------------------------------------------- */
    /* 4.  Position‑only arguments (after options)                     */
    /* ------------------------------------------------------------------- */
    for (int arg = optind; arg < argc; ++arg) {
        /* argv[arg] is a positional argument; store or process it as needed */
        /* In the original program this was appended to an array within v4 */
        /* Here we simply print them for illustration. */
        printf("Positional argument: %s\n", argv[arg]);
    }

    /* ------------------------------------------------------------------- */
    /* 5.  Cleanup: Free all dynamically allocated strings              */
    /* ------------------------------------------------------------------- */
    free(opts.config_file);
    free(opts.output_file);
    free(opts.time_expr);
    free(opts.locale);
    free(opts.user);
    free(opts.group);
    /* ...plus any other fields not nil ... */

    /* Return 0 on success; original function returned the options struct.
     * Returning 0 is sufficient for this rewrite. */
    return 0;
}

/* ----------------------------------------------------------------------- */
/*                   Helper function implementations                       */
/* ----------------------------------------------------------------------- */

/* Dummy implementation. In the real binary this would
 * fetch a string from the embedded string table. */
static const char *lookup_str(const char *id)
{
    /* Placeholder: simply return the ID for now. */
    return id;
}

/* Helper that prints the program's help text. */
static void print_help(void)
{
    puts("Usage: program [options] [args]");
    puts("Options:");
    /* The original had ~300 fprintf calls; we use puts/printf for brevity. */
    puts("  -h, --help              Show this help");
    puts("  -v, --verbose          Enable verbose mode");
    puts("  -q, --quiet            Suppress normal output");
    puts("      --config FILE      Specify configuration file");
    puts("  -o FILE                Write output to FILE");
    puts("      --timeexpr EXPR    Set a custom time expression");
    puts("      --locale LANG      Set the locale");
    puts("      --user USER        Run as USER");
    puts("      --group GROUP      Run with GROUP");
    puts("  --dry-run              Perform a trial run with no changes");
    puts("  --force                Enable forceful operations");
    /* ... */
}

/* Simple var‑argument string helper – not used in the current code but
 * would be needed for some of the original argument parsing.
 */
static const char *append_str(const char *fmt, ...)
{
    va_list va;
    va_start(va, fmt);
    char *buf = NULL;
    vasprintf(&buf, fmt, va);
    va_end(va);
    return buf;
}
```
