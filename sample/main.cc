#include <iostream>
#include <string>

int main() {
    std::cout << "Hello from Bazel remote cache!" << std::endl;
    std::cout << "This build should be cached for subsequent runs." << std::endl;
    return 0;
}
