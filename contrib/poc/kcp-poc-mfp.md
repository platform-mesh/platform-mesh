# KCP and Micro Frontend Platform (MFP) Integration POC

## Overview

This POC demonstrates the integration between KCP (Kubernetes Control Planes) and a Micro Frontend Platform (MFP), showcasing how workload management and frontend services can be orchestrated across multiple workspaces and clusters.

## Architecture

### KCP Workspace Structure

![KCP Workspace Structure](../assets/apeirora-poc-design-infrav1.png)

The KCP instance is organized into several key workspaces:

1. **Root Workspace**
   - Base workspace that contains all other workspaces
   - Manages global configurations and policies

2. **Organizations Workspace**
   - Contains organization-level accounts
   - Example: `apeirora` account

3. **Apeirora Workspace**
   - Organization-specific workspace
   - Contains example account configurations

4. **Example Workspace**
   - Contains workload definitions (e.g., HTTPBin service)
   - Includes APIBinding for MSP integration
   - Manages synchronization with external clusters

5. **MSP Workspace**
   - Managed Service Provider workspace
   - Contains APIExport and APIResourceSchema definitions
   - Facilitates resource sharing and management

### MSP Control Plane Integration

The MSP Control Plane provides the actual Kubernetes cluster where workloads are deployed:

- Namespace: `prefix-apeirora-example`
- Workload: HTTPBin service deployment
- Bidirectional sync with KCP example workspace

### Micro Frontend Platform Integration

The MFP architecture consists of:

1. **OpenMFP Cluster**
   - Account UI for user management
   - Gateway for routing
   - Integration with KCP through account-operator

2. **MSP Cluster**
   - Consumer UI for end-users
   - Operator UI for administrators
   - HTTPBin operator for service management

## Example Service: HTTPBin

The POC uses HTTPBin as an example service to demonstrate:

![HTTPBin Integration](../assets/apeirora-poc-design-resources.png)


1. **Resource Definition**
   - Defined in KCP example workspace
   - Synchronized to MSP cluster

2. **Deployment Flow**
   - KCP manages the service definition
   - MSP cluster handles actual deployment
   - Bidirectional synchronization ensures consistency

3. **Access Management**
   - APIBinding in example workspace connects to MSP services
   - Resource schema defined in MSP workspace
   - Controlled access through workspace hierarchy

## Key Features

1. **Multi-tenant Isolation**
   - Separate workspaces for different organizations
   - Isolated resource management

2. **Centralized Control**
   - KCP provides unified control plane
   - Consistent resource management across clusters

3. **Frontend Integration**
   - MFP provides user interfaces for different personas
   - Seamless integration with KCP resources

4. **Managed Services**
   - MSP provides actual compute resources
   - Automated synchronization with KCP definitions

## Implementation Details

1. **Workspace Hierarchy**
   ```
   root
   ├── organizations
   │   └── apeirora
   ├── apeirora
   │   └── example
   └── msp
   ```

2. **Resource Synchronization**
   - KCP to MSP: Service definitions and configurations
   - MSP to KCP: Status updates and health information

3. **Frontend Components**
   - Account management UI
   - Service consumer interface
   - Operator dashboard
   - HTTPBin specific controls

This POC demonstrates how KCP can be used to manage complex, multi-cluster deployments while integrating with a micro frontend platform for comprehensive service management and user interaction.
