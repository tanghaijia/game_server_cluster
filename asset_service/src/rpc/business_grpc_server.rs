use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::{
    domain::{Game, Node, NodeAgent},
    error::AssetServiceError,
    ports::{GameRepository, NodeAgentRepository, NodeRepository, SteamBranchRepository},
    proto::asset_service::{
        self, CreateGameRequest, CreateGameResponse, CreateNodeRequest, CreateNodeResponse,
        DeleteGameRequest, DeleteGameResponse, DeleteNodeRequest, DeleteNodeResponse,
        GetGameRequest, GetGameResponse, GetNodeAgentRequest, GetNodeAgentResponse, GetNodeRequest,
        GetNodeResponse, ListGamesRequest, ListGamesResponse, ListNodeAgentsRequest,
        ListNodeAgentsResponse, ListNodesRequest, ListNodesResponse, ListSteamBranchesRequest,
        ListSteamBranchesResponse, RegisterNodeAgentRequest, RegisterNodeAgentResponse,
        UnregisterNodeAgentRequest, UnregisterNodeAgentResponse, UpdateGameRequest,
        UpdateGameResponse, UpdateNodeAgentRequest, UpdateNodeAgentResponse, UpdateNodeRequest,
        UpdateNodeResponse, business_service_server::BusinessService as BusinessRpc,
    },
};

pub struct GrpcBusinessService<G, N, A, S>
where
    G: GameRepository,
    N: NodeRepository,
    A: NodeAgentRepository,
    S: SteamBranchRepository,
{
    games: Arc<G>,
    nodes: Arc<N>,
    agents: Arc<A>,
    steam_branches: Arc<S>,
}

impl<G, N, A, S> GrpcBusinessService<G, N, A, S>
where
    G: GameRepository,
    N: NodeRepository,
    A: NodeAgentRepository,
    S: SteamBranchRepository,
{
    pub fn new(games: Arc<G>, nodes: Arc<N>, agents: Arc<A>, steam_branches: Arc<S>) -> Self {
        Self {
            games,
            nodes,
            agents,
            steam_branches,
        }
    }
}

#[tonic::async_trait]
impl<G, N, A, S> BusinessRpc for GrpcBusinessService<G, N, A, S>
where
    G: GameRepository + 'static,
    N: NodeRepository + 'static,
    A: NodeAgentRepository + 'static,
    S: SteamBranchRepository + 'static,
{
    // ==================== Game CRUD ====================

    async fn create_game(
        &self,
        request: Request<CreateGameRequest>,
    ) -> Result<Response<CreateGameResponse>, Status> {
        let game = map_game_from_proto(
            request
                .into_inner()
                .game
                .ok_or_else(|| Status::invalid_argument("game is required"))?,
        );
        self.games.save(&game).await.map_err(map_error)?;
        Ok(Response::new(CreateGameResponse {
            game: Some(map_game_to_proto(&game)),
        }))
    }

    async fn get_game(
        &self,
        request: Request<GetGameRequest>,
    ) -> Result<Response<GetGameResponse>, Status> {
        let game = self
            .games
            .get(&request.into_inner().id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("game not found"))?;
        Ok(Response::new(GetGameResponse {
            game: Some(map_game_to_proto(&game)),
        }))
    }

    async fn update_game(
        &self,
        request: Request<UpdateGameRequest>,
    ) -> Result<Response<UpdateGameResponse>, Status> {
        let game = map_game_from_proto(
            request
                .into_inner()
                .game
                .ok_or_else(|| Status::invalid_argument("game is required"))?,
        );
        // 确认存在
        self.games
            .get(&game.id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("game not found"))?;
        self.games.save(&game).await.map_err(map_error)?;
        Ok(Response::new(UpdateGameResponse {
            game: Some(map_game_to_proto(&game)),
        }))
    }

    async fn delete_game(
        &self,
        request: Request<DeleteGameRequest>,
    ) -> Result<Response<DeleteGameResponse>, Status> {
        let id = request.into_inner().id;
        self.games
            .get(&id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("game not found"))?;
        self.games.delete(&id).await.map_err(map_error)?;
        Ok(Response::new(DeleteGameResponse {}))
    }

    async fn list_games(
        &self,
        _request: Request<ListGamesRequest>,
    ) -> Result<Response<ListGamesResponse>, Status> {
        let games = self.games.list().await.map_err(map_error)?;
        Ok(Response::new(ListGamesResponse {
            games: games.into_iter().map(|g| map_game_to_proto(&g)).collect(),
        }))
    }

    // ==================== Node CRUD ====================

    async fn create_node(
        &self,
        request: Request<CreateNodeRequest>,
    ) -> Result<Response<CreateNodeResponse>, Status> {
        let node = map_node_from_proto(
            request
                .into_inner()
                .node
                .ok_or_else(|| Status::invalid_argument("node is required"))?,
        );
        self.nodes.save(&node).await.map_err(map_error)?;
        Ok(Response::new(CreateNodeResponse {
            node: Some(map_node_to_proto(&node)),
        }))
    }

    async fn get_node(
        &self,
        request: Request<GetNodeRequest>,
    ) -> Result<Response<GetNodeResponse>, Status> {
        let node = self
            .nodes
            .get(&request.into_inner().id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node not found"))?;
        Ok(Response::new(GetNodeResponse {
            node: Some(map_node_to_proto(&node)),
        }))
    }

    async fn update_node(
        &self,
        request: Request<UpdateNodeRequest>,
    ) -> Result<Response<UpdateNodeResponse>, Status> {
        let node = map_node_from_proto(
            request
                .into_inner()
                .node
                .ok_or_else(|| Status::invalid_argument("node is required"))?,
        );
        self.nodes
            .get(&node.id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node not found"))?;
        self.nodes.save(&node).await.map_err(map_error)?;
        Ok(Response::new(UpdateNodeResponse {
            node: Some(map_node_to_proto(&node)),
        }))
    }

    async fn delete_node(
        &self,
        request: Request<DeleteNodeRequest>,
    ) -> Result<Response<DeleteNodeResponse>, Status> {
        let id = request.into_inner().id;
        self.nodes
            .get(&id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node not found"))?;
        self.nodes.delete(&id).await.map_err(map_error)?;
        Ok(Response::new(DeleteNodeResponse {}))
    }

    async fn list_nodes(
        &self,
        _request: Request<ListNodesRequest>,
    ) -> Result<Response<ListNodesResponse>, Status> {
        let nodes = self.nodes.list().await.map_err(map_error)?;
        Ok(Response::new(ListNodesResponse {
            nodes: nodes.into_iter().map(|n| map_node_to_proto(&n)).collect(),
        }))
    }

    // ==================== NodeAgent CRUD ====================

    async fn register_node_agent(
        &self,
        request: Request<RegisterNodeAgentRequest>,
    ) -> Result<Response<RegisterNodeAgentResponse>, Status> {
        let agent = map_agent_from_proto(
            request
                .into_inner()
                .agent
                .ok_or_else(|| Status::invalid_argument("agent is required"))?,
        );
        self.agents.save(&agent).await.map_err(map_error)?;
        Ok(Response::new(RegisterNodeAgentResponse {
            agent: Some(map_agent_to_proto(&agent)),
        }))
    }

    async fn get_node_agent(
        &self,
        request: Request<GetNodeAgentRequest>,
    ) -> Result<Response<GetNodeAgentResponse>, Status> {
        let agent = self
            .agents
            .get(&request.into_inner().node_id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node agent not found"))?;
        Ok(Response::new(GetNodeAgentResponse {
            agent: Some(map_agent_to_proto(&agent)),
        }))
    }

    async fn update_node_agent(
        &self,
        request: Request<UpdateNodeAgentRequest>,
    ) -> Result<Response<UpdateNodeAgentResponse>, Status> {
        let agent = map_agent_from_proto(
            request
                .into_inner()
                .agent
                .ok_or_else(|| Status::invalid_argument("agent is required"))?,
        );
        self.agents
            .get(&agent.node_id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node agent not found"))?;
        self.agents.save(&agent).await.map_err(map_error)?;
        Ok(Response::new(UpdateNodeAgentResponse {
            agent: Some(map_agent_to_proto(&agent)),
        }))
    }

    async fn unregister_node_agent(
        &self,
        request: Request<UnregisterNodeAgentRequest>,
    ) -> Result<Response<UnregisterNodeAgentResponse>, Status> {
        let node_id = request.into_inner().node_id;
        self.agents
            .get(&node_id)
            .await
            .map_err(map_error)?
            .ok_or_else(|| Status::not_found("node agent not found"))?;
        self.agents.delete(&node_id).await.map_err(map_error)?;
        Ok(Response::new(UnregisterNodeAgentResponse {}))
    }

    async fn list_node_agents(
        &self,
        _request: Request<ListNodeAgentsRequest>,
    ) -> Result<Response<ListNodeAgentsResponse>, Status> {
        let agents = self.agents.list().await.map_err(map_error)?;
        Ok(Response::new(ListNodeAgentsResponse {
            agents: agents.into_iter().map(|a| map_agent_to_proto(&a)).collect(),
        }))
    }

    // ==================== SteamBranch ====================

    async fn list_steam_branches(
        &self,
        request: Request<ListSteamBranchesRequest>,
    ) -> Result<Response<ListSteamBranchesResponse>, Status> {
        let game_id = request.into_inner().game_id;
        if game_id.is_empty() {
            return Err(Status::invalid_argument("game_id is required"));
        }
        let branches = self
            .steam_branches
            .get_branches(&game_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(ListSteamBranchesResponse {
            branches: branches
                .into_iter()
                .map(|b| map_steam_branch_to_proto(&b))
                .collect(),
        }))
    }
}

// ==================== Mapping Helpers ====================

fn map_error(error: AssetServiceError) -> Status {
    match error {
        AssetServiceError::InvalidRequest { message } => Status::invalid_argument(message),
        AssetServiceError::BuildNotFound { build_id } => Status::not_found(build_id),
        AssetServiceError::SnapshotNotFound { snapshot_id } => Status::not_found(snapshot_id),
        AssetServiceError::ModManifestNotFound { manifest_id } => Status::not_found(manifest_id),
        AssetServiceError::NodeNotFound { node_id } => Status::not_found(node_id),
        AssetServiceError::NodeAgentNotFound { node_id } => Status::not_found(node_id),
        AssetServiceError::Conflict { message } => Status::failed_precondition(message),
        AssetServiceError::Internal { message } => Status::internal(message),
    }
}

fn map_game_from_proto(value: asset_service::Game) -> Game {
    Game {
        id: value.id,
        name: value.name,
        app_id: value.app_id,
    }
}

fn map_game_to_proto(value: &Game) -> asset_service::Game {
    asset_service::Game {
        id: value.id.clone(),
        name: value.name.clone(),
        app_id: value.app_id.clone(),
    }
}

fn map_node_from_proto(value: asset_service::Node) -> Node {
    Node {
        id: value.id,
        ip: value.ip,
        core_num: value.core_num,
        core_frequency: value.core_frequency,
        memory_size: value.memory_size,
        storage_size: value.storage_size,
        location: value.location,
        service_provider: value.service_provider,
        status: value.status,
    }
}

fn map_node_to_proto(value: &Node) -> asset_service::Node {
    asset_service::Node {
        id: value.id.clone(),
        ip: value.ip.clone(),
        core_num: value.core_num,
        core_frequency: value.core_frequency,
        memory_size: value.memory_size,
        storage_size: value.storage_size,
        location: value.location.clone(),
        service_provider: value.service_provider.clone(),
        status: value.status.clone(),
    }
}

fn map_agent_from_proto(value: asset_service::NodeAgent) -> NodeAgent {
    NodeAgent {
        node_id: value.node_id,
        endpoint: value.endpoint,
        status: value.status,
        last_heartbeat_at: value.last_heartbeat_at,
    }
}

fn map_agent_to_proto(value: &NodeAgent) -> asset_service::NodeAgent {
    asset_service::NodeAgent {
        node_id: value.node_id.clone(),
        endpoint: value.endpoint.clone(),
        status: value.status.clone(),
        last_heartbeat_at: value.last_heartbeat_at,
    }
}

// ==================== SteamBranch Mapping ====================

fn map_steam_branch_to_proto(value: &crate::ports::SteamBranch) -> asset_service::SteamBranch {
    asset_service::SteamBranch {
        name: value.name.clone(),
        build_id: value.build_id,
        description: value.description.clone(),
        app_id: value.app_id.clone(),
        manifests: value
            .manifests
            .iter()
            .map(|m| map_depot_manifest_to_proto(m))
            .collect(),
    }
}

fn map_depot_manifest_to_proto(
    value: &crate::ports::DepotManifest,
) -> asset_service::DepotManifest {
    asset_service::DepotManifest {
        depot_id: value.depot_id,
        manifest_gid: value.manifest_gid,
    }
}
