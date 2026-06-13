package repository

import (
	"context"
	"errors"
)

func consumeTicketNotUpdated(
	ctx context.Context,
	r *ResolutionTicketRepo,
	id string,
	now int64,
) (ResolutionTicket, error) {
	ticket, err := r.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrResolutionTicketNotFound) {
			return ResolutionTicket{}, ErrResolutionTicketNotFound
		}
		return ResolutionTicket{}, err
	}
	if ticket.ConsumedAt != nil {
		return ResolutionTicket{}, ErrResolutionTicketConsumed
	}
	if ticket.ExpiresAt <= now {
		return ResolutionTicket{}, ErrResolutionTicketExpired
	}
	return ResolutionTicket{}, ErrResolutionTicketNotFound
}
