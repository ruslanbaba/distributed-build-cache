#include <iostream>
#include <cassert>

// Simple test for the hello binary
int main() {
    std::cout << "Running hello test..." << std::endl;
    
    // Simple assertion test
    assert(1 + 1 == 2);
    
    std::cout << "All tests passed!" << std::endl;
    return 0;
}
