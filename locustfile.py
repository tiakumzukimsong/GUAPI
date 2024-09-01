import os
from locust import HttpUser, task, between
import random
import string

class UploadTestUser(HttpUser):
    wait_time = between(1, 3)  # Wait time between tasks

    @task
    def upload_image(self):
        # Path to an image you want to upload
        image_path = "./image-demo/cameraman.jpeg"  # Replace with your actual image file

        # Ensure the file exists
        if not os.path.exists(image_path):
            print(f"Image file '{image_path}' not found.")
            return

        # Read the image file
        with open(image_path, "rb") as image_file:
            image_name = ''.join(random.choices(string.ascii_lowercase + string.digits, k=8)) + ".jpg"
            files = {
                'files': (image_name, image_file, 'image/jpeg')
            }

            # Make the POST request to the /upload endpoint
            with self.client.post("/upload", files=files, name="Upload Image", catch_response=True) as response:
                if response.status_code == 200:
                    response.success()
                else:
                    response.failure(f"Failed to upload image. Status code: {response.status_code}")

    def on_start(self):
        """ Called when a Locust user starts before any task is scheduled """
        print("Starting the test.")

    def on_stop(self):
        """ Called when the TaskSet is stopping """
        print("Test completed.")
