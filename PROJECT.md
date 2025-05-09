# Tiny Tunnel Project Documentation

## Overview

Tiny Tunnel is a lightweight tunneling tool that allows exposing local services to the internet. This project documentation focuses on the implementation of a Terminal User Interface (TUI) using Charmbracelet's Bubbletea framework, along with a state management system to improve visibility and control over tunnel operations.

## Features Implemented

### 1. Terminal User Interface (TUI)

We've created a full-screen interactive TUI that displays:

- Connection status (connected, disconnected, connecting) with color-coded indicators
- Tunnel details (name, target URL, public URL)
- Real-time metrics (requests, responses, WebSocket connections)
- Status messages and connection duration
- Log viewing capability

The TUI provides keyboard shortcuts:
- `q` - Quit the application
- `l` - Toggle log view mode
- `o` - Open the tunnel URL in the default browser

### 2. State Management System

We've implemented a centralized state management system that is deliberately separate from the TUI:

- Tracks tunnel connection state independently of any UI
- Collects performance metrics from the core tunnel functionality
- Provides a single source of truth for all components (client, TUI, future UIs)
- Follows a publisher-subscriber pattern for state updates
- Completely separates business logic from presentation logic
- Enables multiple different UIs to share the same underlying state
- Acts as a clear boundary between the tunnel core and its presentation

### 3. Log Capture and Display

A log capture system has been implemented that:

- Redirects log output to the TUI
- Parses and displays log messages in a readable format
- Captures important system messages like the welcome message
- Updates connection status based on log content

### 4. Enhanced User Experience

Several UX improvements were made:

- Automatic URL detection from welcome messages
- Visual indicators of tunnel status
- Real-time metrics display
- Full-screen interface with responsive layout
- Reliable SIGINT (Ctrl+C) handling

## Architecture

The implementation follows a layered architecture:

1. **Core Layer**
   - `shared/tunnel.go` - Base tunnel implementation
   - `protocol/message.go` - Message protocol definitions
   
2. **Business Logic Layer**
   - `stats/tunnel_state.go` - State management
   - `client/client.go` - Client implementation
   
3. **Presentation Layer**
   - `client/ui/tui.go` - TUI implementation
   - `client/ui/log_capture.go` - Log capture and parsing

4. **Command Layer**
   - `cmd/start.go` - Command line interface

## Challenges and Lessons Learned

### 1. Concurrency Issues

**Challenge**: Managing multiple goroutines between the TUI, tunnel listener, and state updates caused subtle race conditions and deadlocks.

**Solution**: We used channels for communication between components and added proper synchronization with mutex locks in state manager.

**Lessons**:
- Always use proper synchronization for shared state
- Consider using channels for communication between components
- Be careful with goroutines that share resources

### 2. TUI Initialization

**Challenge**: Starting the TUI before the tunnel connection was established caused issues with state representation.

**Solution**: We changed the flow to establish the connection first, then initialize the TUI with the known state.

**Lessons**:
- Initialize UI components after core functionality is working
- Pay attention to the initialization order of components
- Use a staged initialization approach for complex systems

### 3. Logging Interference

**Challenge**: Redirecting logs to the TUI interfered with connection establishment and operation.

**Solution**: Added a more sophisticated log capture system and carefully managed when log redirection happens.

**Lessons**:
- Be careful when redirecting standard outputs
- Design logging systems that don't interfere with core functionality
- Use separate logging channels for different components

### 4. State Management and UI Separation

**Challenge**: Keeping the TUI state synchronized with the actual tunnel state proved difficult, and initial attempts tightly coupled the UI with the tunnel functionality.

**Solution**: Implemented a separate, dedicated state manager (TunnelState) that sits between the core functionality and any UI. This state manager:
- Centralizes all state transitions and metrics collection
- Provides a clean API for updating state from any component
- Uses an observer pattern to notify UIs of changes
- Implements the client.Provider interface to capture metrics
- Remains completely independent of any UI implementation

**Lessons**:
- Strictly separate state management from UI components
- Use a single source of truth for state
- Define clear interfaces between state and UI layers
- Allow the state manager to be consumed by multiple UIs
- Implement proper observer patterns for state updates
- Clearly define state transitions and responsibilities
- Design state manager to be UI-agnostic from the start

### 5. Message Handling

**Challenge**: Ensuring all protocol messages were properly handled and responses were sent correctly.

**Solution**: Added extensive logging and verification of message handling paths.

**Lessons**:
- Log all critical message paths
- Verify handler registration
- Monitor both incoming and outgoing message flows

## Things to Avoid

1. **Avoid Complex Initial Design**
   - Start with minimal functionality and iteratively add features
   - Test each component separately before integration

2. **Avoid Premature Optimization**
   - Focus on correct behavior first, then optimize
   - Profile before optimizing to identify actual bottlenecks

3. **Avoid Tight Coupling**
   - Keep the TUI separate from core business logic
   - Use interfaces and dependency injection

4. **Avoid Global State**
   - Pass state explicitly between components
   - Use structured state management patterns

5. **Avoid Direct Output in Core Logic**
   - Send messages through proper channels
   - Separate logging from business logic

## Future Improvements

1. **Enhanced TUI Features**
   - Add more detailed metrics display
   - Implement scrollable log view
   - Add configuration editing in the TUI

2. **Improved State Management**
   - Add persistence for connection history
   - Implement more sophisticated metrics collection
   - Add state snapshots for debugging

3. **Better Error Handling**
   - Add more specific error states
   - Provide more detailed error information in the TUI
   - Implement recovery mechanisms

4. **Connection Diagnostics**
   - Add network diagnostics tools
   - Implement ping functionality
   - Add automatic reconnect with exponential backoff

5. **Web UI Alternative**
   - Leverage the state manager to build a web UI
   - Use websockets for real-time updates
   - Provide a dashboard for multiple tunnels

## Key Architectural Decision: State Manager Separation

One of the most important architectural decisions in this project was the strict separation of the state manager from any UI components. This separation brings several significant benefits:

1. **Independent Core Functionality**: The tunnel client can operate without any UI, with the state manager still tracking all relevant metrics and state transitions.

2. **UI Flexibility**: Multiple different UIs (TUI, web, GUI) can connect to the same state manager, showing the same real-time state without any modifications to the core functionality.

3. **Testability**: The state manager can be tested independently of any UI, and the UIs can be tested with a mock state manager.

4. **Clear Boundaries**: Having a dedicated state management layer creates clear boundaries of responsibility between components.

5. **Future Extensibility**: Additional UIs or metrics collectors can be added without modifying the core tunnel functionality.

The state manager pattern implemented here follows principles similar to those found in frontend frameworks like Redux or Vuex, but adapted for a Go CLI application. This approach proved invaluable as the complexity of the application grew.

## Conclusion

The implementation of a TUI for Tiny Tunnel significantly improves the user experience by providing real-time visibility into tunnel operations. The state management system decouples the business logic from the presentation, making the code more maintainable and extensible.

While several challenges were encountered, the solutions implemented provide valuable lessons for future development. The architecture established, particularly the separation of the state manager from the UI, provides a solid foundation for continued enhancement of the tool. This separation will facilitate the development of alternative interfaces and ensure that the core functionality remains robust regardless of how it is presented to the user.