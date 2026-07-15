/* Intentionally vulnerable SUID binary — lab use only
   Vuln: system() with unsanitized PATH → PATH hijack → root shell */
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

int main(int argc, char *argv[]) {
    printf("IntraCorp Health Check v1.0\n");
    printf("Running as UID=%d EUID=%d\n", getuid(), geteuid());
    /* Calls 'ps' without absolute path — exploit via PATH manipulation */
    system("ps aux");
    return 0;
}
