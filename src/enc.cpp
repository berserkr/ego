#include <elf.h>
#include <openenclave/advanced/allocator.h>
#include <openenclave/ert.h>
#include <sys/mount.h>
#include <unistd.h>
#include <cassert>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <stdexcept>

static const auto _memfs_name = "edg_memfs";
static const auto _premain_env_key = "EDG_EGO_PREMAIN";

using namespace std;
using namespace ert;
static int _argc;
static char** _argv;

extern "C" void ert_meshentry_premain(int* argc, char*** argv);
static char** _merge_argv_env(int argc, char** argv, char** envp);

extern "C" __thread char ert_ego_reserved_tls[1024];
extern "C" const char* oe_sgx_get_td();

static void _start_main(int payload_main(...))
{
    exit(payload_main(_argc, _argv));
}

int emain()
{
    // Accessing this variable makes sure that the reserved_tls lib will be
    // linked. See comment about the lib in CMakeLists for more info.
    *ert_ego_reserved_tls = 0;
    // Assert that the variable is located at the end of the TLS block.
    assert(
        oe_sgx_get_td() - ert_ego_reserved_tls == sizeof ert_ego_reserved_tls);

    // load oe modules
    if (oe_load_module_host_epoll() != OE_OK ||
        oe_load_module_host_file_system() != OE_OK ||
        oe_load_module_host_resolver() != OE_OK ||
        oe_load_module_host_socket_interface() != OE_OK)
    {
        cout << "oe_load_module_host failed\n";
        return EXIT_FAILURE;
    }

    // check for premain
    const char* const env_is_marblerun = getenv(_premain_env_key);
    const bool is_marblerun = env_is_marblerun && *env_is_marblerun == '1';

    // mount hostfs
    const auto mount_path = is_marblerun ? "/edg/hostfs" : "/";
    if (mount("/", mount_path, OE_HOST_FILE_SYSTEM, 0, nullptr) != 0)
    {
        cout << "mount hostfs failed\n";
        return EXIT_FAILURE;
    }

    const Memfs memfs(_memfs_name);
    // get args and env
    if (is_marblerun)
    {
        cout << "invoking premain\n";
        ert_meshentry_premain(&_argc, &_argv);
        cout << "premain done\n";
        _argv = _merge_argv_env(_argc, _argv, environ);
    }
    else
    {
        _argc = ert_get_argc();
        _argv = ert_get_argv();

        const char* const cwd = getenv("EDG_CWD");
        if (!cwd || !*cwd || chdir(cwd) != 0)
        {
            cout << "cannot set cwd\n";
            return EXIT_FAILURE;
        }
    }

    // relocate
    try
    {
        ert::payload::apply_relocations(_start_main);
    }
    catch (const exception& e)
    {
        cout << "apply_relocations failed: " << e.what() << '\n';
        return EXIT_FAILURE;
    }

    // get payload entry point
    const auto base = static_cast<const uint8_t*>(ert::payload::get_base());
    assert(base);
    const auto& ehdr = *reinterpret_cast<const Elf64_Ehdr*>(base);
    assert(ehdr.e_entry);
    const auto entry = (void (*)())(base + ehdr.e_entry);

    entry();
}

ert_args_t ert_get_args()
{
    //
    // Get env vars from the host.
    //

    ert_args_t args{};
    if (ert_get_args_ocall(&args) != OE_OK || args.envc < 0 || args.argc < 0)
        abort();

    char** env = nullptr;
    ert_copy_strings_from_host_to_enclave(
        args.envp, &env, static_cast<size_t>(args.envc));

    assert(env);

    //
    // Keep all env vars that begin with EDG_
    //

    size_t edg_count = 0;

    for (size_t i = 0; env[i]; ++i)
    {
        if (memcmp(env[i], "EDG_", 4) == 0)
        {
            env[edg_count] = env[i];
            ++edg_count;
        }
    }

    env[edg_count] = nullptr;

    ert_args_t result{};
    result.envc = static_cast<int>(edg_count);
    result.envp = env;

    //
    // Get args from host.
    //

    char** argv = nullptr;
    ert_copy_strings_from_host_to_enclave(
        args.argv, &argv, static_cast<size_t>(args.argc));

    assert(argv);

    result.argv = argv;
    result.argc = args.argc;

    return result;
}

static char** _merge_argv_env(int argc, char** argv, char** envp)
{
    int envc = 0;
    while (envp[envc])
    {
        envc++;
    }

    // [argv][null][env][null][auxv][null]
    char** p = (char**)oe_allocator_calloc(
        (size_t)(argc + 1 + envc + 1 + 2), sizeof *p);

    if (!p)
        abort();

    char** result = p;
    memcpy(p, argv, (size_t)argc * sizeof *argv);
    p += argc + 1;
    memcpy(p, envp, (size_t)envc * sizeof *envp);

    return result;
}
