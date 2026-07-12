import type {
  AssumptionPreferences,
  AssumptionProfile,
  AssumptionProfilesResponse,
  AssumptionValidation,
} from "@/types/api";
import { apiGet, apiPost, apiPut } from "./client";

export function listAssumptionProfiles(): Promise<AssumptionProfilesResponse> {
  return apiGet("/api/v1/simulation-assumptions/profiles");
}

export function getAssumptionProfile(
  id: string,
  version: number,
): Promise<{ profile: AssumptionProfile }> {
  return apiGet(`/api/v1/simulation-assumptions/profiles/${id}/${version}`);
}

export function saveAssumptionProfile(body: {
  profile: AssumptionProfile;
  source_note?: string;
  reviewed_by?: string;
  reviewed_at?: string;
}): Promise<{ profile: AssumptionProfile }> {
  return apiPost("/api/v1/simulation-assumptions/profiles", body);
}

export function validateAssumptionProfile(
  id: string,
  version: number,
  profile: AssumptionProfile,
): Promise<AssumptionValidation> {
  return apiPost(
    `/api/v1/simulation-assumptions/profiles/${id}/${version}/validate`,
    { profile },
  );
}

export function activateAssumptionProfile(
  id: string,
  version: number,
): Promise<{ activated: boolean; default_migrated: boolean }> {
  return apiPost(`/api/v1/simulation-assumptions/profiles/${id}/${version}/activate`);
}

export function getAssumptionPreferences(): Promise<{ preferences: AssumptionPreferences }> {
  return apiGet("/api/v1/simulation-assumptions/preferences");
}

export function setAssumptionPreferences(
  preferences: AssumptionPreferences,
): Promise<{ preferences: AssumptionPreferences }> {
  return apiPut("/api/v1/simulation-assumptions/preferences", { preferences });
}
