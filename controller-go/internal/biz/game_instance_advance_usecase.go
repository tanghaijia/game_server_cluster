package biz

import (
	"context"
	"controller-go/internal/entity"
	assetservicev1 "controller-go/internal/client/assetservice"
	"controller-go/internal/repository"
	"errors"
	"fmt"
)

type GameInstanceAdvanceUseCase struct {
	scheduler        Scheduler
	gameInstanceRepo repository.GameInstanceRepository
	assetClient      *assetservicev1.AssetServiceFaceClient
}

func NewGameInstanceAdvanceUseCase(scheduler Scheduler, gameInstanceRepo repository.GameInstanceRepository, assetClient *assetservicev1.AssetServiceFaceClient) *GameInstanceAdvanceUseCase {
	return &GameInstanceAdvanceUseCase{
		scheduler:        scheduler,
		gameInstanceRepo: gameInstanceRepo,
		assetClient:      assetClient,
	}
}

func (uc *GameInstanceAdvanceUseCase) AdvanceGameInstance(ctx context.Context, gameInstance *entity.GameInstance) error {
	switch gameInstance.Status {
	case entity.StatusPending:
		gameInstance.Status = entity.StatusScheduling
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	case entity.StatusScheduling:
		result, err := uc.scheduler.Schedule(ctx, gameInstance)
		if err != nil {
			return err
		}
		if result.Outcome != OutcomeScheduled {
			return errors.New("schedule failed: " + result.Reason)
		}
		gameInstance.NodeAgentID = &result.NodeAgentID
		gameInstance.ResourceReq = &result.ResourceReq
		gameInstance.Status = entity.StatusPreparingBuild
		uc.gameInstanceRepo.Save(ctx, gameInstance)
	case entity.StatusPreparingBuild:
		gameInstance.Status = entity.StatusRestoringSnapshot
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	case entity.StatusRestoringSnapshot:
		gameInstance.Status = entity.StatusStarting
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	case entity.StatusStarting:
		gameInstance.Status = entity.StatusRunning
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	case entity.StatusStopping:
		gameInstance.Status = entity.StatusCleaning
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	case entity.StatusCleaning:
		gameInstance.Status = entity.StatusStopped
		uc.gameInstanceRepo.UpdateStatus(ctx, gameInstance)
	default:
		fmt.Printf("Instance %s is in status %d, cannot advance\n", gameInstance.ID, gameInstance.Status)
		return errors.New("cannot advance from current status")
	}

	return nil
}
