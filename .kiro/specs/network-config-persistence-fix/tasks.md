# Implementation Plan

- [x] 1. Create enhanced configuration management system
  - Implement ConfigurationManager struct with proper Viper integration
  - Add methods for loading and saving configuration using Viper
  - Replace hardcoded directory paths with Viper-based configuration loading
  - _Requirements: 1.1, 1.2, 3.1, 3.2_

- [x] 2. Implement network classification service
  - Create NetworkClassificationService struct with ChainListService integration
  - Implement ClassifyNetwork method to determine if network is standard or custom
  - Add ValidateNetworkAgainstChainList method for chainlist validation
  - Implement GenerateNetworkKey method with appropriate prefixes based on classification
  - _Requirements: 2.1, 2.2, 2.3, 4.1, 4.2_

- [x] 3. Create enhanced network manager
  - Implement NetworkManager struct that uses ConfigurationManager and NetworkClassificationService
  - Add AddNetwork method with automatic classification
  - Implement UpdateNetwork and RemoveNetwork methods
  - Create LoadNetworks method that properly loads from Viper configuration
  - _Requirements: 1.3, 2.4, 3.3, 4.3_

- [x] 4. Refactor UI initialization to use proper configuration loading
  - Update initNetworkList function to use ConfigurationManager instead of hardcoded paths
  - Modify initAddNetwork function to use proper configuration loading
  - Replace direct config.LoadConfig calls with ConfigurationManager usage
  - _Requirements: 1.1, 1.2, 1.3, 3.1_

- [x] 5. Update network addition workflow with classification
  - Modify updateAddNetwork function to use NetworkManager for adding networks
  - Implement automatic network classification during addition
  - Add chainlist validation before classifying as custom
  - Update network key generation to use proper prefixes
  - _Requirements: 2.1, 2.2, 2.3, 4.1, 4.2_

- [x] 6. Enhance network list display with type indicators
  - Update NetworkListComponent to show network type (standard/custom)
  - Add visual indicators for different network types
  - Implement tooltips or labels showing network source (chainlist/manual)
  - _Requirements: 2.4, 4.4_

- [x] 7. Implement error handling for offline chainlist scenarios
  - Add fallback behavior when chainlist API is unavailable
  - Implement proper error messages for network validation failures
  - Add user warnings when networks are added as custom due to chainlist unavailability
  - _Requirements: 4.4, 3.4_

- [ ] 8. Update configuration saving to maintain Viper compatibility
  - Modify saveConfigToFile function to use ConfigurationManager
  - Ensure saved configuration maintains Viper format compatibility
  - Add network type and source metadata to saved configuration
  - _Requirements: 3.2, 3.3_

- [ ] 9. Create unit tests for configuration management
  - Write tests for ConfigurationManager loading and saving
  - Test proper Viper integration and directory resolution
  - Add tests for configuration migration from old format
  - _Requirements: 1.1, 1.2, 3.1, 3.2_

- [ ] 10. Create unit tests for network classification
  - Write tests for NetworkClassificationService classification logic
  - Test chainlist validation and fallback scenarios
  - Add tests for network key generation with proper prefixes
  - _Requirements: 2.1, 2.2, 2.3, 4.1, 4.2_

- [ ] 11. Implement integration tests for network persistence
  - Test network configuration persistence across application restarts
  - Verify proper loading of mixed standard and custom networks
  - Test configuration migration scenarios
  - _Requirements: 1.3, 2.4, 3.3_

- [ ] 12. Add backward compatibility for existing configurations
  - Implement automatic migration of existing network configurations
  - Add detection and classification of networks without type metadata
  - Ensure existing custom networks are properly preserved
  - _Requirements: 1.3, 2.4, 3.3_